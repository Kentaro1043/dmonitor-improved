package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConnectFormatsCallsignArguments(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "bin", "dmonitor")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var", "www"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidFiles := []string{
		filepath.Join(root, "var", "run", "dmonitor.pid"),
		filepath.Join(root, "var", "run", "dmonitor", "pid"),
		filepath.Join(root, "var", "tmp", "dmonitor.pid"),
	}
	for _, pidFile := range pidFiles {
		if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pidFile, []byte("999999\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$DMONITOR_ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	qemu := filepath.Join(root, "fake-qemu")
	if err := os.WriteFile(qemu, []byte(`#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -L) shift 2 ;;
    -E) shift 2 ;;
    *) exec "$@" ;;
  esac
done
`), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "args.txt")
	t.Setenv("DMONITOR_ARGS_FILE", argsFile)

	manager := NewManager(Options{RootFS: root, QEMUPath: qemu})
	if err := manager.Connect(context.Background(), Connection{
		ConnectCallsign: "jl1iza a",
		Address:         "203.0.113.10",
		Port:            "51000",
		AreaCallsign:    "jp1aaa a",
		ZoneCallsign:    "jp1aaa",
	}); err != nil {
		t.Fatal(err)
	}

	var got []byte
	var err error
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		got, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	want := "JL1IZA A\n203.0.113.10\n51000\nJP1AAA A\nJP1AAA  \n"
	if string(got) != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	for _, pidFile := range pidFiles {
		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Fatalf("stale pid file %s still exists, stat error = %v", pidFile, err)
		}
	}
}

func TestConnectRejectsBlankConnectCallsign(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(Options{RootFS: root, QEMUPath: "false"})
	err := manager.Connect(context.Background(), Connection{
		Address:      "203.0.113.10",
		Port:         "51000",
		AreaCallsign: "JP1AAA A",
	})
	if err == nil || !strings.Contains(err.Error(), "connect callsign is required") {
		t.Fatalf("Connect() error = %v", err)
	}
}

func TestUnmanagedExecutableProcessIsDiscoveredAndStopped(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "bin", "dmonitor")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrap 'exit 0' INT TERM\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	manager := NewManager(Options{RootFS: root, QEMUPath: "false"})
	var state ProcessState
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		state = manager.Snapshot().Processes["dmonitor"]
		if state.Running && state.PID == cmd.Process.Pid {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !state.Running || state.PID != cmd.Process.Pid {
		t.Fatalf("unmanaged process state = %+v, want pid %d running", state, cmd.Process.Pid)
	}
	if err := manager.Stop(context.Background(), "dmonitor", time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("unmanaged process pid %d is still running", cmd.Process.Pid)
	}
}

func TestDaemonizedProcessGroupStaysManaged(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "usr", "bin", "dmonitor")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var", "www"), 0o755); err != nil {
		t.Fatal(err)
	}
	childPIDFile := filepath.Join(root, "child.pid")
	script := `#!/bin/sh
(trap 'exit 0' INT TERM; while :; do sleep 1; done) &
echo $! > "$DMONITOR_CHILD_PID_FILE"
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	qemu := filepath.Join(root, "fake-qemu")
	if err := os.WriteFile(qemu, []byte(`#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -L) shift 2 ;;
    -E) shift 2 ;;
    *) exec "$@" ;;
  esac
done
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DMONITOR_CHILD_PID_FILE", childPIDFile)

	manager := NewManager(Options{RootFS: root, QEMUPath: qemu})
	if err := manager.Start(context.Background(), "dmonitor"); err != nil {
		t.Fatal(err)
	}
	childPID := waitForPIDFile(t, childPIDFile)
	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
	})

	var state ProcessState
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		state = manager.Snapshot().Processes["dmonitor"]
		if state.Running && state.ExitCode == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !state.Running {
		t.Fatalf("daemonized dmonitor is not managed as running: %+v", state)
	}
	if state.ExitCode != nil {
		t.Fatalf("daemonized dmonitor exposed parent exit code: %+v", state)
	}

	if err := manager.Stop(context.Background(), "dmonitor", time.Second); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if err := syscall.Kill(childPID, 0); errorsIsProcessDone(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("daemonized child pid %d is still running", childPID)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	var lastErr error
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		b, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr != nil {
				t.Fatal(convErr)
			}
			return pid
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("read pid file %s: %v", path, lastErr)
	return 0
}

func errorsIsProcessDone(err error) bool {
	if err == nil || err == syscall.EPERM {
		return false
	}
	if err == syscall.ESRCH {
		return true
	}
	return false
}
