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
