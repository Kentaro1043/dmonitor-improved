package runtime

import "testing"

func TestParseRepeaterText(t *testing.T) {
	got := parseRepeaterText(`<tr><td>JP1AAA A</td><td>JP1AAA G</td><td>203.0.113.10</td><td>51000</td></tr>`)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].AreaCallsign != "JP1AAA A" || got[0].ZoneCallsign != "JP1AAA G" || got[0].Address != "203.0.113.10" || got[0].Port != "51000" {
		t.Fatalf("unexpected repeater: %+v", got[0])
	}
}

func TestParseRepeaterJSON(t *testing.T) {
	input := []byte(`{"Connected Table":[
{"callsign":"JP1YCD A","ip_address":"27.91.220.53","port":51000,"status":"off","area":"1","zr_call":"JP1YCD  "},
{"callsign":"JP1YKL A","ip_address":"110.1.104.144","port":51000,"status":"off","area":"1",,"ur_call":"CQCQCQ  ","zr_call":"JP1YKL  "}
]}`)
	got := parseRepeaterJSON(input, map[string]string{"JP1YCD A": "JP1YCD A世田谷430"})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].AreaCallsign != "JP1YCD A" || got[0].Address != "27.91.220.53" || got[0].Port != "51000" || got[0].ZoneCallsign != "JP1YCD" {
		t.Fatalf("unexpected repeater: %+v", got[0])
	}
	if got[0].Name != "JP1YCD A世田谷430" {
		t.Fatalf("name = %q", got[0].Name)
	}
}

func TestParseMonitorLinks(t *testing.T) {
	input := `<a href="/cgi-bin/monitor?ip_addr=203.0.113.10&port=51000&callsign='JP1AAA A'&rep_name='Tokyo'&zr_call='JP1AAA  '" target="cmd1">JP1AAA A</a>`
	got := parseRepeaterText(input)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].AreaCallsign != "JP1AAA A" || got[0].Address != "203.0.113.10" || got[0].Port != "51000" || got[0].ZoneCallsign != "JP1AAA" {
		t.Fatalf("unexpected repeater: %+v", got[0])
	}
}
