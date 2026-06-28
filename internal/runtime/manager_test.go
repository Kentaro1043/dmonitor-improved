package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
