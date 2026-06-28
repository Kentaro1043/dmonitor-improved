package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var compatibilityFiles = map[string]string{
	"connected_table.html": "",
	"repeater_active.html": "",
	"repeater_mon.html":    "",
	"repeater_scan.html":   "",
	"error_msg.html":       "",
	"short_msg.html":       "",
}

type Repeater struct {
	AreaCallsign string `json:"areaCallsign"`
	ZoneCallsign string `json:"zoneCallsign,omitempty"`
	Address      string `json:"address,omitempty"`
	Port         string `json:"port,omitempty"`
	Name         string `json:"name,omitempty"`
	Raw          string `json:"raw"`
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

func ParseRepeaters(rootfs string) []Repeater {
	paths := []string{
		filepath.Join(rootfs, "var", "tmp", "repeater_mon.html"),
		filepath.Join(rootfs, "var", "www", "rpt_mast.txt"),
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			return parseRepeaterText(string(b))
		}
	}
	return nil
}

func parseRepeaterText(input string) []Repeater {
	text := stripTags(input)
	lines := strings.Split(text, "\n")
	out := make([]Repeater, 0)
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		r := parseRepeaterLine(line)
		if r.AreaCallsign != "" || r.Address != "" {
			out = append(out, r)
		}
	}
	return out
}

var (
	htmlTagRE  = regexp.MustCompile(`<[^>]+>`)
	callRE     = regexp.MustCompile(`\b[A-Z0-9]{1,2}[0-9][A-Z0-9]{1,5}(?: [A-Z])?\b`)
	ipRE       = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	portRE     = regexp.MustCompile(`\b[1-9][0-9]{3,5}\b`)
	htmlEntity = strings.NewReplacer("&nbsp;", " ", "&lt;", "<", "&gt;", ">", "&amp;", "&")
)

func stripTags(input string) string {
	input = strings.ReplaceAll(input, "<br>", "\n")
	input = strings.ReplaceAll(input, "<br/>", "\n")
	input = strings.ReplaceAll(input, "<br />", "\n")
	input = htmlTagRE.ReplaceAllString(input, " ")
	return htmlEntity.Replace(input)
}

func parseRepeaterLine(line string) Repeater {
	upper := strings.ToUpper(line)
	calls := callRE.FindAllString(upper, -1)
	ips := ipRE.FindAllString(line, -1)
	ports := portRE.FindAllString(line, -1)
	r := Repeater{Raw: line}
	if len(calls) > 0 {
		r.AreaCallsign = calls[0]
	}
	if len(calls) > 1 {
		r.ZoneCallsign = calls[1]
	}
	if len(ips) > 0 {
		r.Address = ips[0]
	}
	if len(ports) > 0 {
		r.Port = ports[0]
	}
	r.Name = line
	return r
}
