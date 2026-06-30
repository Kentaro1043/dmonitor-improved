package device

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Status struct {
	DevicePath    string `json:"devicePath"`
	DStarExists   bool   `json:"dstarExists"`
	DStarTarget   string `json:"dstarTarget,omitempty"`
	TTYACM0Exists bool   `json:"ttyACM0Exists"`
	VendorID      string `json:"vendorId,omitempty"`
	ProductID     string `json:"productId,omitempty"`
	Message       string `json:"message"`
}

func Detect(devicePath string) Status {
	devicePath = normalizeDevicePath(devicePath)
	st := Status{DevicePath: devicePath}
	if _, err := os.Lstat(devicePath); err == nil {
		st.DStarExists = true
		if target, err := filepath.EvalSymlinks(devicePath); err == nil && target != devicePath {
			st.DStarTarget = target
		}
		st.VendorID, st.ProductID = readUSBIDs(devicePath)
	}
	if devicePath == "/dev/dstar" && fileExists("/dev/ttyACM0") {
		st.TTYACM0Exists = true
		st.VendorID, st.ProductID = readUSBIDs("/dev/ttyACM0")
	}
	switch {
	case st.DStarExists:
		if devicePath == "/dev/dstar" {
			st.Message = "/dev/dstar is available"
		} else {
			st.Message = "Configured D-STAR device is available: " + devicePath
		}
	case st.TTYACM0Exists:
		st.Message = "/dev/ttyACM0 is available. Install the udev rule to create /dev/dstar."
	default:
		if devicePath == "/dev/dstar" {
			st.Message = "No /dev/dstar or /dev/ttyACM0 device was found."
		} else {
			st.Message = "Configured D-STAR device was not found: " + devicePath
		}
	}
	return st
}

func normalizeDevicePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/dev/dstar"
	}
	return path
}

func readUSBIDs(device string) (string, string) {
	base, err := filepath.EvalSymlinks(device)
	if err != nil {
		base = device
	}
	name := filepath.Base(base)
	for _, candidate := range []string{
		filepath.Join("/sys/class/tty", name, "device"),
		filepath.Join("/sys/class/tty", filepath.Base(device), "device"),
	} {
		vendor, product := walkUSB(candidate)
		if vendor != "" || product != "" {
			return vendor, product
		}
	}
	return "", ""
}

func walkUSB(start string) (string, string) {
	current, err := filepath.EvalSymlinks(start)
	if err != nil {
		return "", ""
	}
	for {
		vendor := readTrim(filepath.Join(current, "idVendor"))
		product := readTrim(filepath.Join(current, "idProduct"))
		if vendor != "" || product != "" {
			return vendor, product
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ""
		}
		current = parent
	}
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}

func UdevHint(devicePath string) string {
	devicePath = normalizeDevicePath(devicePath)
	if runtime.GOOS == "darwin" {
		return "macOS does not allow creating /dev/dstar directly. Start with -dstar-device /dev/cu.<device> or set DMONITOR_DSTAR_DEVICE=/dev/cu.<device>."
	}
	if devicePath != "/dev/dstar" {
		return "The configured host device is mapped to the guest /dev/dstar path by the compatibility preload layer."
	}
	return "Install udev/99-dmonitor.rules to /etc/udev/rules.d/, then run: sudo udevadm control --reload-rules && sudo udevadm trigger"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
