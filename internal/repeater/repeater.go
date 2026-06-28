package repeater

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Repeater struct {
	AreaCallsign string `json:"areaCallsign"`
	ZoneCallsign string `json:"zoneCallsign,omitempty"`
	Address      string `json:"address,omitempty"`
	Port         string `json:"port,omitempty"`
	Status       string `json:"status,omitempty"`
	Area         string `json:"area,omitempty"`
	Name         string `json:"name,omitempty"`
	Active       bool   `json:"active"`
	Raw          string `json:"raw"`
}

func ParseRepeaters(rootfs string) []Repeater {
	names := parseRepeaterNames(rootfs)
	jsonRepeaters := parseRepeaterJSONFile(filepath.Join(rootfs, "var", "www", "repeater.json"), names)
	jsonByKey := repeaterMap(jsonRepeaters)
	activeByKey := repeaterMap(ParseActiveRepeaters(rootfs))

	paths := []string{
		filepath.Join(rootfs, "var", "tmp", "repeater_mon.temp"),
		filepath.Join(rootfs, "var", "tmp", "repeater_mon.html"),
		filepath.Join(rootfs, "var", "www", "rpt_mast.txt"),
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			repeaters := parseRepeaterText(string(b))
			if len(repeaters) > 0 {
				enrichRepeaterNames(repeaters, names)
				enrichRepeaterMetadata(repeaters, jsonByKey, activeByKey)
				return repeaters
			}
		}
	}
	enrichRepeaterMetadata(jsonRepeaters, nil, activeByKey)
	return jsonRepeaters
}

func ParseActiveRepeaters(rootfs string) []Repeater {
	names := parseRepeaterNames(rootfs)
	for _, path := range []string{
		filepath.Join(rootfs, "var", "tmp", "repeater_active.temp"),
		filepath.Join(rootfs, "var", "tmp", "repeater_active.html"),
		filepath.Join(rootfs, "var", "tmp", "connected_table.html"),
	} {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			continue
		}
		if strings.Contains(string(b), "ありません") {
			return []Repeater{}
		}
		repeaters := parseRepeaterText(string(b))
		if len(repeaters) == 0 {
			continue
		}
		enrichRepeaterNames(repeaters, names)
		for idx := range repeaters {
			repeaters[idx].Active = true
			if repeaters[idx].Status == "" {
				repeaters[idx].Status = "active"
			}
		}
		return repeaters
	}
	return []Repeater{}
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
	r.Area = areaFromCallsign(r.AreaCallsign)
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
			Area:         normalizeArea(strings.TrimSpace(item.Area), item.Callsign),
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
		r.Area = areaFromCallsign(r.AreaCallsign)
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

func enrichRepeaterMetadata(repeaters []Repeater, jsonByKey map[string]Repeater, activeByKey map[string]Repeater) {
	for idx := range repeaters {
		key := repeaterKey(repeaters[idx])
		if meta, ok := jsonByKey[key]; ok {
			hasAddress := repeaters[idx].Address != ""
			if repeaters[idx].Address == "" {
				repeaters[idx].Address = meta.Address
			}
			if repeaters[idx].Port == "" || !hasAddress {
				repeaters[idx].Port = meta.Port
			}
			if repeaters[idx].ZoneCallsign == "" || !hasAddress {
				repeaters[idx].ZoneCallsign = meta.ZoneCallsign
			}
			if repeaters[idx].Status == "" {
				repeaters[idx].Status = meta.Status
			}
			if repeaters[idx].Area == "" {
				repeaters[idx].Area = meta.Area
			}
		}
		if active, ok := activeByKey[key]; ok {
			repeaters[idx].Active = true
			if repeaters[idx].Status == "" || repeaters[idx].Status == "off" {
				repeaters[idx].Status = "active"
			}
			if repeaters[idx].Name == "" {
				repeaters[idx].Name = active.Name
			}
		}
		if repeaters[idx].Area == "" {
			repeaters[idx].Area = areaFromCallsign(repeaters[idx].AreaCallsign)
		}
	}
}

func repeaterMap(repeaters []Repeater) map[string]Repeater {
	out := make(map[string]Repeater, len(repeaters))
	for _, repeater := range repeaters {
		if key := repeaterKey(repeater); key != "" {
			out[key] = repeater
		}
	}
	return out
}

func repeaterKey(repeater Repeater) string {
	return normalizeRepeaterKey(repeater.AreaCallsign)
}

func normalizeArea(area, callsign string) string {
	if area != "" {
		return area
	}
	return areaFromCallsign(callsign)
}

func areaFromCallsign(callsign string) string {
	callsign = strings.Join(strings.Fields(strings.ToUpper(callsign)), "")
	for _, r := range callsign {
		if r >= '0' && r <= '9' {
			return string(r)
		}
	}
	return ""
}

func normalizeRepeaterKey(value string) string {
	return strings.Join(strings.Fields(strings.ToUpper(value)), " ")
}
