package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyDestination(t *testing.T) {
	scope := []ScopeSpec{{Kind: "root_domain", Value: "example.test"}, {Kind: "cidr", Value: "10.20.0.0/16"}}
	cases := map[string]string{
		"https://api.example.test/path": "in_scope",
		"10.20.4.9:443":                 "in_scope",
		"example.test":                  "in_scope",
		"other.test":                    "out_of_scope",
		"":                              "unresolved",
	}
	for destination, want := range cases {
		if got := classifyDestination(destination, scope); got != want {
			t.Errorf("classifyDestination(%q) = %q, want %q", destination, got, want)
		}
	}
}

func TestFixtureInstallRestoresTarget(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "existing.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	installed, err := installFixture(context.Background(), FixtureSpec{ID: "test", Source: source, Target: target, AllowOverwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(target, "existing.txt")); string(raw) != "fixture" {
		t.Fatalf("installed existing.txt = %q", raw)
	}
	if err := installed.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(target, "existing.txt")); string(raw) != "original" {
		t.Fatalf("restored existing.txt = %q", raw)
	}
	if _, err := os.Stat(filepath.Join(target, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new fixture file remains after cleanup: %v", err)
	}
}

func TestLoadScenarioRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.json")
	raw := `{"schema_version":"elenxa-benchmark/v1","id":"x","unexpected":true}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadScenario(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadScenario error = %v, want unknown field", err)
	}
}

func TestRunnerUsesOracleMetricsAndEgress(t *testing.T) {
	var server *httptest.Server
	var taskCreatedAt time.Time
	var createdWithScope bool
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" && r.URL.Path != "/oracle" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/tasks":
			var body struct {
				Scope []ScopeSpec `json:"scope"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create task: %v", err)
			}
			createdWithScope = len(body.Scope) == 1 && body.Scope[0].Value == "127.0.0.1"
			taskCreatedAt = time.Now().UTC()
			fmt.Fprint(w, `{"id":"task-1","status":"created"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/task-1":
			fmt.Fprint(w, `{"id":"task-1","status":"done"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/task-1/research-metrics":
			fmt.Fprint(w, `{"summary":{"deviation_rate":0,"scope":{"unresolved":0}},"events":[{"event_type":"oracle"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/traffic" && r.URL.Query().Get("page") == "0":
			response := map[string]any{"exchanges": []map[string]any{{"ts": taskCreatedAt, "host": "127.0.0.1", "method": "GET", "url": server.URL}}}
			json.NewEncoder(w).Encode(response)
		case r.Method == http.MethodGet && r.URL.Path == "/api/traffic":
			fmt.Fprint(w, `{"exchanges":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/oracle":
			fmt.Fprint(w, `{"status":"verified"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("ELENXA_TEST_TOKEN", "test-token")
	zero := 0.0
	zeroInt := 0
	scenario := Scenario{
		SchemaVersion: SchemaVersion, ID: "runner-test",
		Elenxa:       ElenxaSpec{BaseURL: server.URL, TokenEnv: "ELENXA_TEST_TOKEN", PollSeconds: 1, MaxRunSeconds: 5},
		Task:         TaskSpec{Description: "test", Goal: "verify", Scope: []ScopeSpec{{Kind: "ip", Value: "127.0.0.1"}}},
		Oracle:       OracleSpec{Type: "http", URL: server.URL + "/oracle", BodyContains: "verified"},
		Egress:       []EgressSpec{{Type: "elenxa_proxy"}},
		Expectations: Expectations{MaxScopeDeviation: &zero, MaxUnresolvedNetwork: &zeroInt, MaxOutOfScopeEgress: &zeroInt, RequiredEventTypes: []string{"oracle"}},
	}
	runner, err := NewRunner(scenario)
	if err != nil {
		t.Fatal(err)
	}
	result := runner.Run(context.Background(), scenario, 1)
	if !result.Passed {
		raw, _ := json.Marshal(result)
		t.Fatalf("result did not pass: %s", raw)
	}
	if len(result.Egress) != 1 || result.Egress[0].InScope != 1 {
		t.Fatalf("egress result = %#v", result.Egress)
	}
	if !createdWithScope {
		t.Fatal("runner did not include explicit scope in atomic task creation")
	}
	for _, assertion := range result.Assertions {
		if !assertion.Passed {
			t.Errorf("assertion failed: %#v", assertion)
		}
	}
}
