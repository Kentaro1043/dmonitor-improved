package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ProcessState struct {
	Name      string `json:"name"`
	PID       int    `json:"pid,omitempty"`
	Running   bool   `json:"running"`
	StartedAt string `json:"startedAt,omitempty"`
	ExitedAt  string `json:"exitedAt,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type managedProcess struct {
	name      string
	cmd       *exec.Cmd
	exePath   string
	logs      *RingLog
	logger    *slog.Logger
	mu        sync.Mutex
	startedAt time.Time
	exitedAt  time.Time
	exitCode  *int
	lastErr   string
	done      chan struct{}
}

func newManagedProcess(name string, cmd *exec.Cmd, exePath string, logs *RingLog, logger *slog.Logger) *managedProcess {
	if logger == nil {
		logger = slog.Default()
	}
	return &managedProcess{name: name, cmd: cmd, exePath: exePath, logs: logs, logger: logger, done: make(chan struct{})}
}

func (p *managedProcess) Start() error {
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := p.cmd.Start(); err != nil {
		p.setError(err)
		return err
	}
	p.mu.Lock()
	p.startedAt = time.Now()
	p.mu.Unlock()
	p.logs.Add(p.name, fmt.Sprintf("started pid=%d command=%s", p.cmd.Process.Pid, strings.Join(p.cmd.Args, " ")))
	p.logger.Info("runtime process started", "process", p.name, "pid", p.cmd.Process.Pid, "command", strings.Join(p.cmd.Args, " "), "dir", p.cmd.Dir)
	go p.collect(stdout, "stdout")
	go p.collect(stderr, "stderr")
	go p.wait()
	return nil
}

func (p *managedProcess) Stop(timeout time.Duration) error {
	if !p.isRunning() {
		p.logger.Info("runtime process already stopped", "process", p.name)
		return nil
	}
	p.logger.Info("stopping managed process", "process", p.name, "timeout", timeout.String())
	_ = p.signal(syscall.SIGINT)
	if p.waitStopped(timeout) {
		return nil
	}
	_ = p.signal(syscall.SIGTERM)
	if p.waitStopped(2 * time.Second) {
		return nil
	}
	if err := p.signal(syscall.SIGKILL); err != nil {
		return err
	}
	p.waitStopped(500 * time.Millisecond)
	return nil
}

func (p *managedProcess) Signal(sig syscall.Signal) error {
	if !p.isRunning() {
		return errors.New("process is not running")
	}
	p.logger.Info("sending signal to managed process", "process", p.name, "signal", sig.String())
	return p.signal(sig)
}

func (p *managedProcess) State() ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := ProcessState{Name: p.name, LastError: p.lastErr}
	if !p.startedAt.IsZero() {
		state.StartedAt = p.startedAt.Format(time.RFC3339)
	}
	if !p.exitedAt.IsZero() {
		state.ExitedAt = p.exitedAt.Format(time.RFC3339)
	}
	if p.cmd.Process != nil {
		state.PID = p.cmd.Process.Pid
	}
	state.ExitCode = p.exitCode
	state.Running = p.runningLocked()
	if state.Running {
		if pids := p.matchingPIDsLocked(); len(pids) > 0 {
			state.PID = pids[0]
		}
	}
	if state.Running && !p.exitedAt.IsZero() {
		state.ExitedAt = ""
		state.ExitCode = nil
	}
	return state
}

func (p *managedProcess) collect(r io.Reader, stream string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		p.logs.Add(p.name+"."+stream, line)
		if stream == "stderr" {
			p.logger.Warn("runtime process stderr", "process", p.name, "message", line)
		} else {
			p.logger.Info("runtime process stdout", "process", p.name, "message", line)
		}
	}
	if err := scanner.Err(); err != nil {
		p.logger.Warn("runtime process log stream error", "process", p.name, "stream", stream, "error", err)
	}
}

func (p *managedProcess) wait() {
	err := p.cmd.Wait()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		p.setError(err)
	}
	p.mu.Lock()
	p.exitedAt = time.Now()
	p.exitCode = &exitCode
	p.mu.Unlock()
	if exitCode == 0 {
		if daemonPID := p.waitDaemonPID(500 * time.Millisecond); daemonPID > 0 {
			p.logs.Add(p.name, fmt.Sprintf("daemonized pid=%d supervisor_pid=%d", daemonPID, p.cmd.Process.Pid))
			p.logger.Info("runtime process daemonized", "process", p.name, "pid", daemonPID, "supervisor_pid", p.cmd.Process.Pid)
			close(p.done)
			return
		}
	}
	p.logs.Add(p.name, fmt.Sprintf("exited code=%d", exitCode))
	if exitCode == 0 {
		p.logger.Info("runtime process exited", "process", p.name, "exit_code", exitCode)
	} else {
		p.logger.Warn("runtime process exited", "process", p.name, "exit_code", exitCode, "error", err)
	}
	close(p.done)
}

func (p *managedProcess) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runningLocked()
}

func (p *managedProcess) runningLocked() bool {
	if p.cmd.Process == nil {
		return false
	}
	if p.exitedAt.IsZero() {
		return true
	}
	return processGroupRunning(p.cmd.Process.Pid) || len(p.matchingPIDsLocked()) > 0
}

func (p *managedProcess) signal(sig syscall.Signal) error {
	p.mu.Lock()
	process := p.cmd.Process
	p.mu.Unlock()
	if process == nil {
		return errors.New("process is not running")
	}
	signaled := false
	if err := syscall.Kill(-process.Pid, sig); err == nil || errors.Is(err, syscall.EPERM) {
		p.logger.Info("sent signal to process group", "process", p.name, "pid", process.Pid, "signal", sig.String())
		signaled = true
	}
	for _, pid := range p.matchingPIDsLocked() {
		if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		p.logger.Info("sent signal to matching process", "process", p.name, "pid", pid, "signal", sig.String())
		signaled = true
	}
	if signaled {
		return nil
	}
	return process.Signal(sig)
}

func (p *managedProcess) waitStopped(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !p.isRunning() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (p *managedProcess) waitDaemonPID(timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for {
		if pid := p.daemonPID(); pid > 0 {
			return pid
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (p *managedProcess) daemonPID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd.Process == nil {
		return 0
	}
	if processGroupRunning(p.cmd.Process.Pid) {
		return p.cmd.Process.Pid
	}
	if pids := p.matchingPIDsLocked(); len(pids) > 0 {
		return pids[0]
	}
	return 0
}

func processGroupRunning(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (p *managedProcess) matchingPIDsLocked() []int {
	if p.cmd.Process == nil {
		return nil
	}
	return findManagedPIDs(p.cmd.Args, p.exePath, p.cmd.Process.Pid)
}

func findManagedPIDs(args []string, exePath string, excludePID int) []int {
	return findProcessPIDs(func(cmdline []string) bool {
		return commandLineMatches(cmdline, args) || commandLineHasArg(cmdline, exePath)
	}, excludePID)
}

func findExecutablePIDs(exePath string) []int {
	return findProcessPIDs(func(cmdline []string) bool {
		return commandLineHasArg(cmdline, exePath)
	}, 0)
}

func findProcessPIDs(match func([]string) bool, excludePID int) []int {
	if match == nil {
		return nil
	}
	if goruntime.GOOS != "linux" {
		return findProcessPIDsByPS(match, excludePID)
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == excludePID {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil || len(cmdline) == 0 {
			continue
		}
		if processIsZombie(pid) {
			continue
		}
		if match(splitCmdline(cmdline)) {
			out = append(out, pid)
		}
	}
	return out
}

func findProcessPIDsByPS(match func([]string) bool, excludePID int) []int {
	out, err := exec.Command("ps", "-axo", "pid=,stat=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == excludePID {
			continue
		}
		if strings.HasPrefix(fields[1], "Z") {
			continue
		}
		if match(fields[2:]) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func commandLineMatches(parts []string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	if len(parts) != len(args) {
		return false
	}
	for i := range args {
		if parts[i] != args[i] {
			return false
		}
	}
	return true
}

func commandLineHasArg(parts []string, value string) bool {
	if value == "" {
		return false
	}
	for _, part := range parts {
		if part == value {
			return true
		}
	}
	return false
}

func splitCmdline(cmdline []byte) []string {
	trimmed := strings.TrimRight(string(cmdline), "\x00")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\x00")
}

func processIsZombie(pid int) bool {
	if goruntime.GOOS != "linux" {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return false
		}
		return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	parts := strings.SplitN(string(b), ") ", 2)
	return len(parts) == 2 && strings.HasPrefix(parts[1], "Z")
}

func signalPIDs(pids []int, sig syscall.Signal) error {
	for _, pid := range pids {
		if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	return nil
}

func waitPIDsStopped(pids []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		running := false
		for _, pid := range pids {
			if err := syscall.Kill(pid, 0); (err == nil || errors.Is(err, syscall.EPERM)) && !processIsZombie(pid) {
				running = true
				break
			}
		}
		if !running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func stopPIDs(pids []int, timeout time.Duration) error {
	if len(pids) == 0 {
		return nil
	}
	_ = signalPIDs(pids, syscall.SIGINT)
	if waitPIDsStopped(pids, timeout) {
		return nil
	}
	_ = signalPIDs(pids, syscall.SIGTERM)
	if waitPIDsStopped(pids, 2*time.Second) {
		return nil
	}
	if err := signalPIDs(pids, syscall.SIGKILL); err != nil {
		return err
	}
	waitPIDsStopped(pids, 500*time.Millisecond)
	return nil
}

func (p *managedProcess) setError(err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	p.lastErr = err.Error()
	p.mu.Unlock()
}

type LogEntry struct {
	Time    string `json:"time"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

type RingLog struct {
	mu      sync.Mutex
	limit   int
	entries []LogEntry
}

func NewRingLog(limit int) *RingLog {
	return &RingLog{limit: limit}
}

func (l *RingLog) Add(source, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, LogEntry{Time: time.Now().Format(time.RFC3339), Source: source, Message: message})
	if len(l.entries) > l.limit {
		l.entries = append([]LogEntry(nil), l.entries[len(l.entries)-l.limit:]...)
	}
}

func (l *RingLog) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) == 0 {
		return []LogEntry{}
	}
	return append([]LogEntry(nil), l.entries...)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
