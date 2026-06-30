package runtime

import (
	"errors"
	"strings"
	"testing"
)

func TestQEMUNotFoundErrorIncludesConfiguredPath(t *testing.T) {
	err := qemuNotFoundError("/opt/bin/qemu-arm", errors.New("missing"))
	if !strings.Contains(err.Error(), "/opt/bin/qemu-arm") {
		t.Fatalf("error = %q, want configured path", err)
	}
	if !strings.Contains(err.Error(), "DMONITOR_QEMU") {
		t.Fatalf("error = %q, want DMONITOR_QEMU hint", err)
	}
}
