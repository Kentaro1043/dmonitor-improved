package device

import (
	"os"
	"path/filepath"
	"strings"
)

type Status struct {
	DStarExists   bool   `json:"dstarExists"`
	DStarTarget   string `json:"dstarTarget,omitempty"`
	TTYACM0Exists bool   `json:"ttyACM0Exists"`
	VendorID      string `json:"vendorId,omitempty"`
	ProductID     string `json:"productId,omitempty"`
	Message       string `json:"message"`
}

func Detect() Status {
	var st Status
	if _, err := os.Lstat("/dev/dstar"); err == nil {
		st.DStarExists = true
		if target, err := filepath.EvalSymlinks("/dev/dstar"); err == nil {
			st.DStarTarget = target
		}
	}
	if _, err := os.Stat("/dev/ttyACM0"); err == nil {
		st.TTYACM0Exists = true
		st.VendorID, st.ProductID = readUSBIDs("/dev/ttyACM0")
	}
	switch {
	case st.DStarExists:
		st.Message = "/dev/dstar is available"
	case st.TTYACM0Exists:
		st.Message = "/dev/ttyACM0 is available. Install the udev rule to create /dev/dstar."
	default:
		st.Message = "No /dev/dstar or /dev/ttyACM0 device was found."
	}
	return st
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

func UdevHint() string {
	return "Install udev/99-dmonitor.rules to /etc/udev/rules.d/, then run: sudo udevadm control --reload-rules && sudo udevadm trigger"
}
