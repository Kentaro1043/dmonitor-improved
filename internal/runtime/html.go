package runtime

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	Status       string `json:"status,omitempty"`
	Area         string `json:"area,omitempty"`
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
	names := parseRepeaterNames(rootfs)
	if repeaters := parseRepeaterJSONFile(filepath.Join(rootfs, "var", "www", "repeater.json"), names); len(repeaters) > 0 {
		return repeaters
	}

	paths := []string{
		filepath.Join(rootfs, "var", "tmp", "repeater_mon.html"),
		filepath.Join(rootfs, "var", "tmp", "repeater_mon.temp"),
		filepath.Join(rootfs, "var", "www", "rpt_mast.txt"),
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			repeaters := parseRepeaterText(string(b))
			if len(repeaters) > 0 {
				enrichRepeaterNames(repeaters, names)
				return repeaters
			}
		}
	}
	return nil
}

func parseRepeaterText(input string) []Repeater {
	if repeaters := parseMonitorLinks(input); len(repeaters) > 0 {
		return repeaters
	}
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
	monitorRE  = regexp.MustCompile(`/cgi-bin/monitor\?([^">]+)`)
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

type repeaterJSON struct {
	ConnectedTable []repeaterJSONEntry `json:"Connected Table"`
}

type repeaterJSONEntry struct {
	Callsign  string `json:"callsign"`
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`
	Status    string `json:"status"`
	Area      string `json:"area"`
	ZRCall    string `json:"zr_call"`
}

func parseRepeaterJSONFile(path string, names map[string]string) []Repeater {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	return parseRepeaterJSON(b, names)
}

func parseRepeaterJSON(b []byte, names map[string]string) []Repeater {
	var payload repeaterJSON
	if err := json.Unmarshal(b, &payload); err != nil {
		fixed := regexp.MustCompile(`,\s*,+`).ReplaceAll(b, []byte(","))
		if err := json.Unmarshal(fixed, &payload); err != nil {
			return nil
		}
	}
	out := make([]Repeater, 0, len(payload.ConnectedTable))
	for _, item := range payload.ConnectedTable {
		r := Repeater{
			AreaCallsign: strings.TrimSpace(item.Callsign),
			ZoneCallsign: strings.TrimSpace(item.ZRCall),
			Address:      strings.TrimSpace(item.IPAddress),
			Port:         strconv.Itoa(item.Port),
			Status:       strings.TrimSpace(item.Status),
			Area:         strings.TrimSpace(item.Area),
			Raw:          strings.TrimSpace(item.Callsign),
		}
		if item.Port == 0 {
			r.Port = ""
		}
		r.Name = names[normalizeRepeaterKey(item.Callsign)]
		if r.Name == "" {
			r.Name = r.AreaCallsign
		}
		if r.AreaCallsign != "" && r.Address != "" {
			out = append(out, r)
		}
	}
	return out
}

func parseMonitorLinks(input string) []Repeater {
	matches := monitorRE.FindAllStringSubmatch(input, -1)
	out := make([]Repeater, 0, len(matches))
	seen := make(map[string]bool)
	for _, match := range matches {
		values := parseQuery(match[1])
		r := Repeater{
			AreaCallsign: strings.Trim(values["callsign"], "' "),
			ZoneCallsign: strings.Trim(values["zr_call"], "' "),
			Address:      values["ip_addr"],
			Port:         values["port"],
			Name:         strings.Trim(values["rep_name"], "' "),
			Raw:          match[0],
		}
		key := r.AreaCallsign + "|" + r.Address + "|" + r.Port
		if r.AreaCallsign == "" || r.Address == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func parseQuery(raw string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(raw, "&") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if unescaped, err := url.QueryUnescape(value); err == nil {
			value = unescaped
		}
		out[key] = htmlEntity.Replace(value)
	}
	return out
}

func parseRepeaterNames(rootfs string) map[string]string {
	b, err := os.ReadFile(filepath.Join(rootfs, "var", "www", "rpt_mast.txt"))
	if err != nil {
		return nil
	}
	names := make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		r := parseRepeaterLine(line)
		if r.AreaCallsign != "" {
			names[normalizeRepeaterKey(r.AreaCallsign)] = line
		}
	}
	return names
}

func enrichRepeaterNames(repeaters []Repeater, names map[string]string) {
	for idx := range repeaters {
		if repeaters[idx].Name == "" || repeaters[idx].Name == repeaters[idx].Raw {
			if name := names[normalizeRepeaterKey(repeaters[idx].AreaCallsign)]; name != "" {
				repeaters[idx].Name = name
			}
		}
	}
}

func normalizeRepeaterKey(value string) string {
	return strings.Join(strings.Fields(strings.ToUpper(value)), " ")
}
