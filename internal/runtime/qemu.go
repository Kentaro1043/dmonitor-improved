package runtime

import (
	"fmt"
	goruntime "runtime"
)

const DefaultQEMUPath = "qemu-arm"

func qemuNotFoundError(qemuPath string, err error) error {
	if qemuPath == "" {
		qemuPath = DefaultQEMUPath
	}
	if goruntime.GOOS == "darwin" {
		return fmt.Errorf("%s not found: %w. QEMU user-mode emulation for Linux binaries is not available on macOS; run dmonitor-improved inside a Linux environment or set -qemu/DMONITOR_QEMU to a working qemu-arm executable", qemuPath, err)
	}
	return fmt.Errorf("%s not found: %w. Install qemu-user or set -qemu/DMONITOR_QEMU to the qemu-arm executable", qemuPath, err)
}
