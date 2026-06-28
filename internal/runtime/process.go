package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	logs      *RingLog
	mu        sync.Mutex
	startedAt time.Time
	exitedAt  time.Time
	exitCode  *int
	lastErr   string
	done      chan struct{}
}

func newManagedProcess(name string, cmd *exec.Cmd, logs *RingLog) *managedProcess {
	return &managedProcess{name: name, cmd: cmd, logs: logs, done: make(chan struct{})}
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
	go p.collect(stdout, "stdout")
	go p.collect(stderr, "stderr")
	go p.wait()
	return nil
}

func (p *managedProcess) Stop(timeout time.Duration) error {
	if !p.isRunning() {
		return nil
	}
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
		p.logs.Add(p.name+"."+stream, scanner.Text())
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
			close(p.done)
			return
		}
	}
	p.logs.Add(p.name, fmt.Sprintf("exited code=%d", exitCode))
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
		signaled = true
	}
	for _, pid := range findCommandPIDs(p.cmd.Args, process.Pid) {
		if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
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
	return findCommandPIDs(p.cmd.Args, p.cmd.Process.Pid)
}

func findCommandPIDs(args []string, excludePID int) []int {
	if len(args) == 0 {
		return nil
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
		if commandLineMatches(cmdline, args) {
			out = append(out, pid)
		}
	}
	return out
}

func commandLineMatches(cmdline []byte, args []string) bool {
	parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
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
