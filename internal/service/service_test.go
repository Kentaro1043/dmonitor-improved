package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Kentaro1043/dmonitor-improved/internal/runtime"
)

func TestStatusUsesEmptyRepeaterLists(t *testing.T) {
	svc := New(runtime.NewManager(runtime.Options{RootFS: t.TempDir(), QEMUPath: "qemu-arm"}))

	status := svc.Status()
	if status.Runtime.Repeaters == nil {
		t.Fatal("repeaters must be an empty slice, not nil")
	}
	if status.Runtime.Active == nil {
		t.Fatal("active repeaters must be an empty slice, not nil")
	}

	b, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"repeaters":[]`)) {
		t.Fatalf("repeaters must be encoded as an empty array: %s", b)
	}
	if !bytes.Contains(b, []byte(`"activeRepeaters":[]`)) {
		t.Fatalf("activeRepeaters must be encoded as an empty array: %s", b)
	}
}
