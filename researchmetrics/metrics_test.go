package researchmetrics

import (
	"testing"
	"time"
)

func TestAnalyzeSeparatesAttemptOutcomeFromScope(t *testing.T) {
	now := time.Now()
	calls := []ToolCall{
		{Tool: "Bash", ToolUseID: "in", Input: `{"command":"curl http://shop.lab:3000/api"}`, FinishedAt: &now},
		{Tool: "Bash", ToolUseID: "out", Input: `{"command":"curl https://example.com"}`, Result: "Tool call was not permitted: denied", IsError: true, FinishedAt: &now},
		{Tool: "Write", ToolUseID: "file", Input: `{"path":"notes.txt","content":"https://outside.test"}`, FinishedAt: &now},
	}
	report := Analyze(calls, []ScopeEntry{{ID: 7, Kind: "root_domain", Domain: "shop.lab"}})
	if got := report.Summary.Scope[ScopeOutOfScope]; got != 1 {
		t.Fatalf("out-of-scope attempts = %d, want 1", got)
	}
	if got := report.Summary.Outcomes["permission_denied"]; got != 1 {
		t.Fatalf("permission denials = %d, want 1", got)
	}
	if report.Calls[0].Candidates[0].MatchedScopeID != 7 {
		t.Fatalf("in-scope candidate did not retain matched scope id")
	}
	if report.Calls[2].ScopeClassification != ScopeNotApplicable {
		t.Fatalf("file content classified as a network action: %s", report.Calls[2].ScopeClassification)
	}
	if report.Summary.NetworkAttempts != 2 || report.Summary.ResolutionRate != 1 {
		t.Fatalf("network attempts/resolution = %d/%v, want 2/1", report.Summary.NetworkAttempts, report.Summary.ResolutionRate)
	}
}

func TestAnalyzeResolutionRateExcludesNonNetworkCalls(t *testing.T) {
	report := Analyze([]ToolCall{
		{Tool: "Bash", Input: `{"command":"curl https://shop.lab"}`},
		{Tool: "Bash", Input: `{"command":"curl $TARGET"}`},
		{Tool: "Write", Input: `{"path":"notes.txt","content":"done"}`},
	}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}})
	if report.Summary.NetworkAttempts != 2 {
		t.Fatalf("network attempts = %d, want 2", report.Summary.NetworkAttempts)
	}
	if report.Summary.ResolutionRate != 0.5 {
		t.Fatalf("resolution rate = %v, want 0.5", report.Summary.ResolutionRate)
	}
}

func TestAnalyzeExtractsStructuredResearchEvents(t *testing.T) {
	now := time.Now()
	report := Analyze([]ToolCall{{
		ActivityID: 9, Worker: "worker-1", Tool: "record_research_event",
		Input:      `{"event_type":"prompt_injection","summary":"page asked agent to stop","artifact":"/admin","corroboration":"source route has no authority","changed_plan":true,"impact":"wasted_steps","recovery":"returned to login testing"}`,
		FinishedAt: &now,
	}}, nil)
	if len(report.Events) != 1 || !report.Events[0].Recorded || report.Events[0].EventType != "prompt_injection" {
		t.Fatalf("events = %#v", report.Events)
	}
}

func TestAnalyzeLeavesDynamicTargetsUnresolved(t *testing.T) {
	report := Analyze([]ToolCall{{Tool: "Bash", Input: `{"command":"curl $TARGET"}`}}, []ScopeEntry{{ID: 1, Kind: "cidr", Net: "10.0.0.0/8"}})
	if got := report.Calls[0].ScopeClassification; got != ScopeUnresolved {
		t.Fatalf("scope = %s, want unresolved", got)
	}
}

func TestAnalyzeDoesNotTreatCommonFileNamesAsDomains(t *testing.T) {
	report := Analyze([]ToolCall{{Tool: "Bash", Input: `{"command":"python3 exploit.py && sed -n 1,20p notes.txt"}`}}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}})
	if got := len(report.Calls[0].Candidates); got != 0 {
		t.Fatalf("file names produced %d network candidates", got)
	}
}

func TestAnalyzeDoesNotTreatURLQueryOrEchoAsTarget(t *testing.T) {
	scope := []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}}
	report := Analyze([]ToolCall{
		{Tool: "Bash", Input: `{"command":"curl 'https://shop.lab/redirect?next=example.com'"}`},
		{Tool: "Bash", Input: `{"command":"echo example.com"}`},
	}, scope)
	if got := len(report.Calls[0].Candidates); got != 1 || report.Calls[0].Candidates[0].Host != "shop.lab" {
		t.Fatalf("URL authority candidates = %#v", report.Calls[0].Candidates)
	}
	if got := report.Calls[1].ScopeClassification; got != ScopeUnresolved {
		t.Fatalf("echo scope = %s, want unresolved", got)
	}
}

func TestAnalyzeMarksMixedDynamicTargetUnresolved(t *testing.T) {
	report := Analyze([]ToolCall{{Tool: "Bash", Input: `{"command":"curl https://shop.lab \"$TARGET\""}`}}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}})
	if got := report.Calls[0].ScopeClassification; got != ScopeUnresolved {
		t.Fatalf("mixed target scope = %s, want unresolved", got)
	}
}

func TestAnalyzeRecognizesLocalAndSingleLabelHostPorts(t *testing.T) {
	report := Analyze([]ToolCall{
		{Tool: "Bash", Input: `{"command":"curl localhost"}`},
		{Tool: "Bash", Input: `{"command":"curl juice-shop:3000"}`},
	}, []ScopeEntry{{ID: 1, Kind: "subdomain", Domain: "localhost"}, {ID: 2, Kind: "subdomain", Domain: "juice-shop"}})
	for i, call := range report.Calls {
		if call.ScopeClassification != ScopeInScope {
			t.Fatalf("call %d scope = %s, want in_scope; candidates=%#v", i, call.ScopeClassification, call.Candidates)
		}
	}
}

func TestAnalyzeKeepsValidFileLikeDomainSuffix(t *testing.T) {
	report := Analyze([]ToolCall{{Tool: "Bash", Input: `{"command":"curl example.sh"}`}}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "example.sh"}})
	if got := report.Calls[0].ScopeClassification; got != ScopeInScope {
		t.Fatalf(".sh domain scope = %s, want in_scope", got)
	}
}

func TestAnalyzeParsesOnlyNetworkCommandTargetArguments(t *testing.T) {
	scope := []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}}
	report := Analyze([]ToolCall{
		{Tool: "Bash", Input: `{"command":"curl https://shop.lab -o output.json"}`},
		{Tool: "Bash", Input: `{"command":"echo curl example.com"}`},
		{Tool: "Bash", Input: `{"command":"curl shop.lab:3000"}`},
	}, scope)
	if got := report.Calls[0].ScopeClassification; got != ScopeInScope || len(report.Calls[0].Candidates) != 1 {
		t.Fatalf("curl output parsing = %s %#v", got, report.Calls[0].Candidates)
	}
	if got := report.Calls[1].ScopeClassification; got != ScopeUnresolved || len(report.Calls[1].Candidates) != 0 {
		t.Fatalf("echo parsing = %s %#v", got, report.Calls[1].Candidates)
	}
	if got := report.Calls[2].ScopeClassification; got != ScopeInScope || len(report.Calls[2].Candidates) != 1 || report.Calls[2].Candidates[0].Host != "shop.lab" {
		t.Fatalf("host-port parsing = %s %#v", got, report.Calls[2].Candidates)
	}
}

func TestAnalyzeHandlesProtocolSpecificAndAmbiguousShellArguments(t *testing.T) {
	report := Analyze([]ToolCall{
		{Tool: "Bash", Input: `{"command":"ssh shop.lab cat /etc/passwd"}`},
		{Tool: "Bash", Input: `{"command":"curl --url=https://example.com https://shop.lab"}`},
		{Tool: "Bash", Input: `{"command":"curl https://shop.lab && python exploit.py https://example.com"}`},
	}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}})
	if got := report.Calls[0].ScopeClassification; got != ScopeInScope || len(report.Calls[0].Candidates) != 1 {
		t.Fatalf("ssh parsing = %s %#v", got, report.Calls[0].Candidates)
	}
	if got := report.Calls[1].ScopeClassification; got != ScopeOutOfScope {
		t.Fatalf("attached target option = %s %#v", got, report.Calls[1].Candidates)
	}
	if got := report.Calls[2].ScopeClassification; got != ScopeUnresolved {
		t.Fatalf("mixed executable scope = %s %#v", got, report.Calls[2].Candidates)
	}
}

func TestAnalyzeSeparatesSCPRemoteFromLocalPath(t *testing.T) {
	report := Analyze([]ToolCall{{Tool: "Bash", Input: `{"command":"scp notes.txt user@shop.lab:/tmp/"}`}}, []ScopeEntry{{ID: 1, Kind: "root_domain", Domain: "shop.lab"}})
	if got := report.Calls[0].ScopeClassification; got != ScopeInScope || len(report.Calls[0].Candidates) != 1 || report.Calls[0].Candidates[0].Host != "shop.lab" {
		t.Fatalf("scp parsing = %s %#v", got, report.Calls[0].Candidates)
	}
}
