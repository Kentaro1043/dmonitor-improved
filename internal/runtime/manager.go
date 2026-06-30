package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Kentaro1043/dmonitor-improved/internal/config"
)

type Options struct {
	RootFS           string
	QEMUPath         string
	GuestPreloadPath string
	DStarDevicePath  string
	Logger           *slog.Logger
}

type Manager struct {
	opts              Options
	logger            *slog.Logger
	mu                sync.Mutex
	procs             map[string]*managedProcess
	logs              *RingLog
	current           *Connection
	lastErr           string
	monitorLoopName   string
	monitorLoopCancel context.CancelFunc
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
}

func NewManager(opts Options) *Manager {
	if opts.QEMUPath == "" {
		opts.QEMUPath = os.Getenv("DMONITOR_QEMU")
	}
	if opts.QEMUPath == "" {
		opts.QEMUPath = DefaultQEMUPath
	}
	if opts.DStarDevicePath == "" {
		opts.DStarDevicePath = os.Getenv("DMONITOR_DSTAR_DEVICE")
	}
	if opts.DStarDevicePath == "" {
		opts.DStarDevicePath = "/dev/dstar"
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		opts:   opts,
		logger: logger,
		procs:  make(map[string]*managedProcess),
		logs:   NewRingLog(500),
	}
}

func (m *Manager) RootFS() string { return m.opts.RootFS }

func (m *Manager) DStarDevicePath() string { return m.opts.DStarDevicePath }

func (m *Manager) LogInventory() {
	qemu, qemuErr := exec.LookPath(m.opts.QEMUPath)
	if qemuErr != nil {
		m.logger.Error("qemu-arm not found", "qemu", m.opts.QEMUPath, "error", qemuNotFoundError(m.opts.QEMUPath, qemuErr))
	} else {
		m.logger.Info("qemu-arm found", "qemu", qemu)
	}
	m.logger.Info("runtime rootfs configured", "rootfs", m.opts.RootFS, "preload", m.opts.GuestPreloadPath, "dstar_device", m.opts.DStarDevicePath)
	for _, name := range managedProcessNames {
		path := m.binPath(name)
		st, err := os.Stat(path)
		if err != nil {
			m.logger.Warn("runtime binary missing", "process", name, "path", path, "error", err)
			continue
		}
		m.logger.Info("runtime binary found", "process", name, "path", path, "mode", st.Mode().String(), "size", st.Size())
	}
	if preload := qemuPreloadPath(m.opts.RootFS, m.opts.GuestPreloadPath); preload == "" {
		m.logger.Warn("compat preload library not found", "configured", m.opts.GuestPreloadPath)
	} else {
		m.logger.Info("compat preload library found", "path", preload)
	}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	processes := make(map[string]ProcessState, len(m.procs))
	for name, proc := range m.procs {
		processes[name] = proc.State()
	}
	for _, name := range managedProcessNames {
		if _, ok := processes[name]; ok {
			continue
		}
		if state := m.unmanagedProcessState(name); state.Running {
			processes[name] = state
		}
	}
	return Snapshot{
		RootFS:     m.opts.RootFS,
		QEMUPath:   m.opts.QEMUPath,
		Processes:  processes,
		Connection: cloneConnection(m.current),
		LastError:  m.lastErr,
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
	m.logger.Info("starting standby receive", "process", "rpt_conn")
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.startRepeaterLoop(false); err != nil {
		return err
	}
	return m.Start(ctx, "rpt_conn")
}

func (m *Manager) StopRPTConn(ctx context.Context) error {
	m.logger.Info("stopping standby receive", "process", "rpt_conn")
	return m.Stop(ctx, "rpt_conn", 10*time.Second)
}

func (m *Manager) Connect(ctx context.Context, req Connection) error {
	m.logger.Info("connecting dmonitor", "address", req.Address, "port", req.Port, "area_callsign", req.AreaCallsign, "zone_callsign", req.ZoneCallsign)
	if req.Port == "" {
		req.Port = "51000"
	}
	if req.ConnectCallsign == "" {
		cfg, _ := config.Load(m.opts.RootFS)
		req.ConnectCallsign = cfg.Callsign
	}
	if strings.TrimSpace(req.ConnectCallsign) == "" {
		return m.recordError(errors.New("connect callsign is required"))
	}
	if strings.TrimSpace(req.AreaCallsign) == "" {
		return m.recordError(errors.New("area callsign is required"))
	}
	req.ConnectCallsign = config.FormatCallsign(req.ConnectCallsign)
	req.AreaCallsign = config.FormatCallsign(req.AreaCallsign)
	if req.ZoneCallsign != "" {
		req.ZoneCallsign = config.FormatCallsign(req.ZoneCallsign)
	}
	args := []string{req.ConnectCallsign, req.Address, req.Port, req.AreaCallsign}
	if req.ZoneCallsign != "" {
		args = append(args, req.ZoneCallsign)
	}
	if err := m.Stop(ctx, "repeater_scan", 5*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Stop(ctx, "rpt_conn", 10*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	m.cleanupPIDFiles()
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
	m.logger.Info("disconnecting dmonitor")
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
	m.logger.Info("starting repeater scan", "process", "repeater_scan")
	if err := m.Stop(ctx, "dmonitor", 15*time.Second); err != nil {
		m.recordError(err)
	}
	if err := m.Stop(ctx, "repeater_scan", 5*time.Second); err != nil {
		m.recordError(err)
	}
	return m.Start(ctx, "repeater_scan")
}

func (m *Manager) StopScan(ctx context.Context) error {
	m.logger.Info("stopping repeater scan", "process", "repeater_scan")
	return m.Stop(ctx, "repeater_scan", 10*time.Second)
}

func (m *Manager) UpdateRepeaters(ctx context.Context) error {
	m.logger.Info("updating repeater files")
	if err := m.downloadFile(ctx, "http://log.d-star.info/usr/rpt_mast.txt", filepath.Join(m.opts.RootFS, "var", "www", "rpt_mast.txt")); err != nil {
		return err
	}
	if err := m.downloadFile(ctx, "http://hole-punchd.d-star.info:30011/repeater.json", filepath.Join(m.opts.RootFS, "var", "www", "repeater.json")); err != nil {
		return err
	}
	m.logs.Add("update", "updated rpt_mast.txt and repeater.json")
	m.logger.Info("updated repeater files")
	return nil
}

func (m *Manager) downloadFile(ctx context.Context, source, path string) error {
	m.logger.Info("downloading runtime data", "source", source, "path", path)
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
	m.logger.Info("downloaded runtime data", "source", source, "path", path, "status", resp.Status)
	return nil
}

func (m *Manager) SignalDMonitor(sig syscall.Signal) error {
	m.logger.Info("signaling dmonitor", "signal", sig.String())
	m.mu.Lock()
	proc := m.procs["dmonitor"]
	m.mu.Unlock()
	if proc != nil && proc.isRunning() {
		return proc.Signal(sig)
	}
	pids := findExecutablePIDs(m.binPath("dmonitor"))
	if len(pids) == 0 {
		return m.recordError(errors.New("dmonitor is not running"))
	}
	return signalPIDs(pids, sig)
}

func (m *Manager) Start(_ context.Context, name string, args ...string) error {
	m.logger.Info("preparing runtime process", "process", name, "args", args)
	if err := EnsureCompatibilityFiles(m.opts.RootFS); err != nil {
		return m.recordError(err)
	}
	bin := filepath.Join(m.opts.RootFS, "usr", "bin", name)
	binStat, err := os.Stat(bin)
	if err != nil {
		return m.recordError(fmt.Errorf("%s is not installed at %s", name, bin))
	}
	m.logger.Info("runtime binary ready", "process", name, "path", bin, "mode", binStat.Mode().String(), "size", binStat.Size())
	qemu, err := exec.LookPath(m.opts.QEMUPath)
	if err != nil {
		return m.recordError(qemuNotFoundError(m.opts.QEMUPath, err))
	}
	m.logger.Info("qemu executable ready", "process", name, "qemu", qemu)

	cmdArgs := []string{"-L", m.opts.RootFS}
	if preload := qemuPreloadPath(m.opts.RootFS, m.opts.GuestPreloadPath); preload != "" {
		cmdArgs = append(cmdArgs, "-E", "LD_PRELOAD="+preload)
		m.logger.Info("using compat preload", "process", name, "preload", preload)
	} else if m.opts.GuestPreloadPath != "" {
		m.logger.Warn("compat preload not found; starting without LD_PRELOAD", "process", name, "configured", m.opts.GuestPreloadPath)
	}
	cmdArgs = append(cmdArgs, "-E", "LD_LIBRARY_PATH="+qemuLibraryPath(m.opts.RootFS))
	cmdArgs = append(cmdArgs,
		"-E", "DMONITOR_HOST_ROOTFS="+m.opts.RootFS,
		"-E", "DMONITOR_DSTAR_DEVICE="+m.opts.DStarDevicePath,
		bin,
	)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(qemu, cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = os.Environ()
	cmd.Dir = filepath.Join(m.opts.RootFS, "var", "www")
	proc := newManagedProcess(name, cmd, bin, m.logs, m.logger)

	m.mu.Lock()
	old := m.procs[name]
	m.procs[name] = proc
	m.mu.Unlock()
	if old != nil {
		m.logger.Info("stopping previous managed process before restart", "process", name)
		_ = old.Stop(5 * time.Second)
	}
	if pids := findExecutablePIDs(bin); len(pids) > 0 {
		m.logger.Warn("stopping unmanaged process before start", "process", name, "pids", pids)
		if err := stopPIDs(pids, 2*time.Second); err != nil {
			return m.recordError(err)
		}
	}
	if err := proc.Start(); err != nil {
		return m.recordError(err)
	}
	return nil
}

func (m *Manager) Stop(_ context.Context, name string, timeout time.Duration) error {
	m.logger.Info("stopping runtime process", "process", name, "timeout", timeout.String())
	m.mu.Lock()
	proc := m.procs[name]
	m.mu.Unlock()
	if proc != nil {
		if err := proc.Stop(timeout); err != nil {
			return m.recordError(err)
		}
	}
	if pids := findExecutablePIDs(m.binPath(name)); len(pids) > 0 {
		m.logger.Warn("stopping unmanaged runtime process", "process", name, "pids", pids, "timeout", timeout.String())
		if err := stopPIDs(pids, timeout); err != nil {
			return m.recordError(err)
		}
	}
	return nil
}

func (m *Manager) Shutdown() {
	m.logger.Info("shutting down runtime manager")
	m.mu.Lock()
	cancel := m.monitorLoopCancel
	m.monitorLoopCancel = nil
	m.monitorLoopName = ""
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, name := range []string{"dmonitor", "rpt_conn", "repeater_scan", "repeater_mon", "repeater_mon_light"} {
		_ = m.Stop(context.Background(), name, 5*time.Second)
	}
}

func (m *Manager) startRepeaterLoop(light bool) error {
	name := "repeater_mon"
	if light {
		name = "repeater_mon_light"
	}
	m.logger.Info("starting repeater monitor loop", "process", name)
	m.mu.Lock()
	if m.monitorLoopCancel != nil && m.monitorLoopName == name {
		m.mu.Unlock()
		m.logger.Info("repeater monitor loop already running", "process", name)
		return m.startIfStopped(context.Background(), name)
	}
	if m.monitorLoopCancel != nil {
		m.logger.Info("replacing repeater monitor loop", "old_process", m.monitorLoopName, "new_process", name)
		m.monitorLoopCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.monitorLoopName = name
	m.monitorLoopCancel = cancel
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		_ = m.startIfStopped(context.Background(), name)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.startIfStopped(context.Background(), name)
			}
		}
	}()
	return nil
}

func (m *Manager) startIfStopped(ctx context.Context, name string) error {
	m.mu.Lock()
	proc := m.procs[name]
	m.mu.Unlock()
	if proc != nil && proc.isRunning() {
		m.logger.Info("runtime process already running", "process", name)
		return nil
	}
	m.logger.Info("runtime process is not running; starting", "process", name)
	return m.Start(ctx, name)
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

func qemuLibraryPath(rootfs string) string {
	paths := []string{
		filepath.Join(rootfs, "usr", "lib"),
		filepath.Join(rootfs, "lib"),
		filepath.Join(rootfs, "usr", "lib", "arm-linux-gnueabihf"),
		filepath.Join(rootfs, "lib", "arm-linux-gnueabihf"),
	}
	return strings.Join(paths, ":")
}

func (m *Manager) cleanupPIDFiles() {
	for _, rel := range []string{"var/run/dmonitor.pid", "var/run/dmonitor/pid", "var/tmp/dmonitor.pid"} {
		_ = os.Remove(filepath.Join(m.opts.RootFS, rel))
	}
}

var managedProcessNames = []string{"dmonitor", "rpt_conn", "repeater_scan", "repeater_mon", "repeater_mon_light"}

func (m *Manager) binPath(name string) string {
	return filepath.Join(m.opts.RootFS, "usr", "bin", name)
}

func (m *Manager) unmanagedProcessState(name string) ProcessState {
	pids := findExecutablePIDs(m.binPath(name))
	if len(pids) == 0 {
		return ProcessState{Name: name}
	}
	return ProcessState{Name: name, PID: pids[0], Running: true}
}

func (m *Manager) recordError(err error) error {
	if err == nil {
		return nil
	}
	m.mu.Lock()
	m.lastErr = err.Error()
	m.mu.Unlock()
	m.logs.Add("error", err.Error())
	m.logger.Error("runtime error", "error", err)
	return err
}

func cloneConnection(in *Connection) *Connection {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
