package evidenceimport

import (
	"strings"
	"testing"
)

func TestParseNmapXML(t *testing.T) {
	raw := `<nmaprun scanner="nmap" version="7.95"><host><address addr="10.0.0.5" addrtype="ipv4"/><ports><port protocol="tcp" portid="443"><state state="open" reason="syn-ack"/><service name="https" product="nginx" version="1.25"/></port></ports></host></nmaprun>`
	result, err := Parse("nmap-xml", "scan.xml", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].Target != "10.0.0.5" || !strings.Contains(result.Observations[0].Evidence, "nginx") {
		t.Fatalf("result=%+v", result)
	}
	if result.Provenance.Digest == "" || result.Provenance.Version != "7.95" {
		t.Fatalf("provenance=%+v", result.Provenance)
	}
}

func TestParseSARIF(t *testing.T) {
	raw := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"scanner","version":"1"}},"results":[{"ruleId":"R1","level":"warning","message":{"text":"unsafe setting"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"app.yaml"},"region":{"startLine":4}}}]}]}]}`
	result, err := Parse("sarif", "result.sarif", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].ID != "R1" || result.Observations[0].Target != "app.yaml:4" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseNucleiJSONLRejectsMalformedLine(t *testing.T) {
	raw := `{"template-id":"x","matched-at":"https://lab.test","info":{"name":"match","severity":"low"}}` + "\n{"
	if _, err := Parse("nuclei-jsonl", "nuclei.jsonl", strings.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error=%v", err)
	}
}
