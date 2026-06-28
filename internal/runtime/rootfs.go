package runtime

import (
	"os"
	"path/filepath"
)

var compatibilityFiles = map[string]string{
	"connected_table.html": "",
	"repeater_active.html": "",
	"repeater_mon.html":    "",
	"repeater_scan.html":   "",
	"error_msg.html":       "",
	"short_msg.html":       "",
}

func EnsureCompatibilityFiles(rootfs string) error {
	tmp := filepath.Join(rootfs, "var", "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	for name, content := range compatibilityFiles {
		path := filepath.Join(tmp, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
