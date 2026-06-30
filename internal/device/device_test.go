package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectConfiguredDevice(t *testing.T) {
	root := t.TempDir()
	devicePath := filepath.Join(root, "tty.dstar")
	if err := os.WriteFile(devicePath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	status := Detect(devicePath)
	if status.DevicePath != devicePath {
		t.Fatalf("DevicePath = %q, want %q", status.DevicePath, devicePath)
	}
	if !status.DStarExists {
		t.Fatalf("DStarExists = false, want true")
	}
	if !strings.Contains(status.Message, devicePath) {
		t.Fatalf("Message = %q, want to contain %q", status.Message, devicePath)
	}
}

func TestDetectMissingConfiguredDevice(t *testing.T) {
	devicePath := filepath.Join(t.TempDir(), "missing")

	status := Detect(devicePath)
	if status.DevicePath != devicePath {
		t.Fatalf("DevicePath = %q, want %q", status.DevicePath, devicePath)
	}
	if status.DStarExists {
		t.Fatalf("DStarExists = true, want false")
	}
	if !strings.Contains(status.Message, devicePath) {
		t.Fatalf("Message = %q, want to contain %q", status.Message, devicePath)
	}
}

func TestDetectNormalizesBlankDevicePath(t *testing.T) {
	status := Detect(" ")
	if status.DevicePath != "/dev/dstar" {
		t.Fatalf("DevicePath = %q, want /dev/dstar", status.DevicePath)
	}
}
