package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	p.mu.Lock()
	process := p.cmd.Process
	p.mu.Unlock()
	if process == nil {
		return nil
	}
	if !p.isRunning() {
		return nil
	}
	_ = process.Signal(syscall.SIGINT)
	select {
	case <-p.done:
		return nil
	case <-time.After(timeout):
		_ = process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		return process.Kill()
	}
}

func (p *managedProcess) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	process := p.cmd.Process
	p.mu.Unlock()
	if process == nil || !p.isRunning() {
		return errors.New("process is not running")
	}
	return process.Signal(sig)
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
	state.Running = p.exitedAt.IsZero() && p.cmd.Process != nil
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
	p.logs.Add(p.name, fmt.Sprintf("exited code=%d", exitCode))
	close(p.done)
}

func (p *managedProcess) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitedAt.IsZero() && p.cmd.Process != nil
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
