package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/config"
)

type Options struct {
	RootFS           string
	QEMUPath         string
	GuestPreloadPath string
}

type Manager struct {
	opts    Options
	mu      sync.Mutex
	procs   map[string]*managedProcess
	logs    *RingLog
	current *Connection
	lastErr string
	stopMon chan struct{}
}

type Connection struct {
	ConnectCallsign string `json:"connectCallsign"`
	Address         string `json:"address"`
	Port            string `json:"port"`
	AreaCallsign    string `json:"areaCallsign"`
	ZoneCallsign    string `json:"zoneCallsign,omitempty"`
	StartedAt       string `json:"startedAt"`
}

type Snapshot struct {
	RootFS     string                  `json:"rootfs"`
	QEMUPath   string                  `json:"qemuPath"`
	Processes  map[string]ProcessState `json:"processes"`
	Connection *Connection             `json:"connection,omitempty"`
	LastError  string                  `json:"lastError,omitempty"`
	Repeaters  []Repeater              `json:"repeaters"`
}

func NewManager(opts Options) *Manager {
	if opts.QEMUPath == "" {
		opts.QEMUPath = "qemu-arm"
	}
	return &Manager{
		opts:    opts,
		procs:   make(map[string]*managedProcess),
		logs:    NewRingLog(500),
		stopMon: make(chan struct{}),
	}
}

func (m *Manager) RootFS() string { return m.opts.RootFS }

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	processes := make(map[string]ProcessState, len(m.procs))
	for name, proc := range m.procs {
		processes[name] = proc.State()
	}
	return Snapshot{
		RootFS:     m.opts.RootFS,
		QEMUPath:   m.opts.QEMUPath,
		Processes:  processes,
		Connection: cloneConnection(m.current),
		LastError:  m.lastErr,
		Repeaters:  ParseRepeaters(m.opts.RootFS),
	}
}

func (m *Manager) Logs() []LogEntry {
	entries := m.logs.Entries()
	for _, path := range []string{
		filepath.Join(m.opts.RootFS, "var", "log", "dmonitor.log"),
		filepath.Join(m.opts.RootFS, "var", "log", "rpt_conn.log"),
		filepath.Join(m.opts.RootFS, "var", "tmp", "update.log"),
		filepath.Join(m.opts.RootFS, "var", "tmp", "error_msg.html"),
		filepath.Join(m.opts.RootFS, "var", "tmp", "short_msg.html"),
	} {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			entries = append(entries, LogEntry{Time: time.Now().Format(time.RFC3339), Source: filepath.Base(path), Message: string(b)})
		}
	}
	if len(entries) == 0 {
		return []LogEntry{}
	}
	return entries
}

func (m *Manager) StartRPTConn(ctx context.Context) error {
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.startRepeaterLoop(false); err != nil {
		return err
	}
	return m.Start(ctx, "rpt_conn")
}

func (m *Manager) StopRPTConn(ctx context.Context) error {
	return m.Stop(ctx, "rpt_conn", 10*time.Second)
}

func (m *Manager) Connect(ctx context.Context, req Connection) error {
	if req.Port == "" {
		req.Port = "51000"
	}
	if req.ConnectCallsign == "" {
		cfg, _ := config.Load(m.opts.RootFS)
		req.ConnectCallsign = cfg.Callsign
	}
	args := []string{req.ConnectCallsign, req.Address, req.Port, req.AreaCallsign}
	if req.ZoneCallsign != "" {
		args = append(args, req.ZoneCallsign)
	}
	if err := m.Stop(ctx, "repeater_scan", 5*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Start(ctx, "dmonitor", args...); err != nil {
		return err
	}
	req.StartedAt = time.Now().Format(time.RFC3339)
	m.mu.Lock()
	m.current = &req
	m.mu.Unlock()
	return nil
}

func (m *Manager) Disconnect(ctx context.Context) error {
	err := m.Stop(ctx, "dmonitor", 15*time.Second)
	m.cleanupPIDFiles()
	m.mu.Lock()
	m.current = nil
	m.mu.Unlock()
	if startErr := m.StartRPTConn(ctx); startErr != nil && err == nil {
		err = startErr
	}
	return err
}

func (m *Manager) StartScan(ctx context.Context) error {
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Stop(ctx, "repeater_scan", 5*time.Second); err != nil {
		m.recordError(err)
	}
	return m.Start(ctx, "repeater_scan")
}

func (m *Manager) StopScan(ctx context.Context) error {
	return m.Stop(ctx, "repeater_scan", 10*time.Second)
}

func (m *Manager) UpdateRepeaters(ctx context.Context) error {
	if err := m.downloadFile(ctx, "http://log.d-star.info/usr/rpt_mast.txt", filepath.Join(m.opts.RootFS, "var", "www", "rpt_mast.txt")); err != nil {
		return err
	}
	if err := m.downloadFile(ctx, "http://hole-punchd.d-star.info:30011/repeater.json", filepath.Join(m.opts.RootFS, "var", "www", "repeater.json")); err != nil {
		return err
	}
	m.logs.Add("update", "updated rpt_mast.txt and repeater.json")
	return nil
}

func (m *Manager) downloadFile(ctx context.Context, source, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Language", "ja-JP")
	req.Header.Set("User-Agent", "dmonitor/02.00")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return m.recordError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return m.recordError(fmt.Errorf("download %s: %s", source, resp.Status))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return m.recordError(err)
	}
	out, err := os.Create(path)
	if err != nil {
		return m.recordError(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return m.recordError(err)
	}
	return nil
}

func (m *Manager) SignalDMonitor(sig syscall.Signal) error {
	m.mu.Lock()
	proc := m.procs["dmonitor"]
	m.mu.Unlock()
	if proc == nil {
		return m.recordError(errors.New("dmonitor is not running"))
	}
	return proc.Signal(sig)
}

func (m *Manager) Start(ctx context.Context, name string, args ...string) error {
	if err := EnsureCompatibilityFiles(m.opts.RootFS); err != nil {
		return m.recordError(err)
	}
	bin := filepath.Join(m.opts.RootFS, "usr", "bin", name)
	if _, err := os.Stat(bin); err != nil {
		return m.recordError(fmt.Errorf("%s is not installed at %s", name, bin))
	}
	qemu, err := exec.LookPath(m.opts.QEMUPath)
	if err != nil {
		return m.recordError(fmt.Errorf("qemu-arm not found: %w", err))
	}

	cmdArgs := []string{"-L", m.opts.RootFS}
	if preload := qemuPreloadPath(m.opts.RootFS, m.opts.GuestPreloadPath); preload != "" {
		cmdArgs = append(cmdArgs, "-E", "LD_PRELOAD="+preload)
	}
	cmdArgs = append(cmdArgs, "-E", "DMONITOR_HOST_ROOTFS="+m.opts.RootFS, bin)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, qemu, cmdArgs...)
	cmd.Env = os.Environ()
	cmd.Dir = filepath.Join(m.opts.RootFS, "var", "www")
	proc := newManagedProcess(name, cmd, m.logs)

	m.mu.Lock()
	old := m.procs[name]
	m.procs[name] = proc
	m.mu.Unlock()
	if old != nil {
		_ = old.Stop(5 * time.Second)
	}
	if err := proc.Start(); err != nil {
		return m.recordError(err)
	}
	return nil
}

func (m *Manager) Stop(_ context.Context, name string, timeout time.Duration) error {
	m.mu.Lock()
	proc := m.procs[name]
	m.mu.Unlock()
	if proc == nil {
		return nil
	}
	if err := proc.Stop(timeout); err != nil {
		return m.recordError(err)
	}
	return nil
}

func (m *Manager) Shutdown() {
	close(m.stopMon)
	for _, name := range []string{"dmonitor", "rpt_conn", "repeater_scan", "repeater_mon", "repeater_mon_light"} {
		_ = m.Stop(context.Background(), name, 5*time.Second)
	}
}

func (m *Manager) startRepeaterLoop(light bool) error {
	name := "repeater_mon"
	if light {
		name = "repeater_mon_light"
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopMon:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				_ = m.Start(ctx, name)
				cancel()
			}
		}
	}()
	return nil
}

func qemuPreloadPath(rootfs, preloadPath string) string {
	if preloadPath == "" {
		return ""
	}
	if filepath.IsAbs(preloadPath) {
		if fileExists(preloadPath) {
			return preloadPath
		}
		candidate := filepath.Join(rootfs, preloadPath)
		if fileExists(candidate) {
			return candidate
		}
		return ""
	}
	candidate := filepath.Join(rootfs, preloadPath)
	if fileExists(candidate) {
		return candidate
	}
	return ""
}

func (m *Manager) cleanupPIDFiles() {
	for _, rel := range []string{"var/run/dmonitor.pid", "var/tmp/dmonitor.pid"} {
		_ = os.Remove(filepath.Join(m.opts.RootFS, rel))
	}
}

func (m *Manager) recordError(err error) error {
	if err == nil {
		return nil
	}
	m.mu.Lock()
	m.lastErr = err.Error()
	m.mu.Unlock()
	m.logs.Add("error", err.Error())
	return err
}

func cloneConnection(in *Connection) *Connection {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
