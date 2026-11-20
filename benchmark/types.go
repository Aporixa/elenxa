// Package benchmark runs reproducible, full-task Elenxa lab evaluations.
// It keeps case generation outside the agent planner while preserving one
// normalized result format for native cases and external generators.
package benchmark

import (
	"encoding/json"
	"time"

	"github.com/Aporixa/elenxa/researchmetrics"
)

const SchemaVersion = "elenxa-benchmark/v1"

type Scenario struct {
	SchemaVersion string       `json:"schema_version"`
	ID            string       `json:"id"`
	Description   string       `json:"description,omitempty"`
	Provenance    *Provenance  `json:"provenance,omitempty"`
	Techniques    []Technique  `json:"techniques,omitempty"`
	Trials        int          `json:"trials,omitempty"`
	Elenxa        ElenxaSpec   `json:"elenxa"`
	Task          TaskSpec     `json:"task"`
	Reset         *CommandSpec `json:"reset,omitempty"`
	Fixture       *FixtureSpec `json:"fixture,omitempty"`
	Oracle        OracleSpec   `json:"oracle"`
	Egress        []EgressSpec `json:"egress,omitempty"`
	Expectations  Expectations `json:"expectations,omitempty"`
}

// Provenance freezes the external case identity without making the external
// tool part of Elenxa's measured runtime.
type Provenance struct {
	Source             string              `json:"source"`
	SourceReference    string              `json:"source_reference,omitempty"`
	SourceVersion      string              `json:"source_version,omitempty"`
	SourceDigest       string              `json:"source_digest"`
	ExternalSuiteID    string              `json:"external_suite_id,omitempty"`
	ExternalCaseID     string              `json:"external_case_id"`
	ExternalCaseDigest string              `json:"external_case_digest"`
	PromptID           string              `json:"prompt_id,omitempty"`
	RenderedInput      string              `json:"rendered_input"`
	Variables          map[string]any      `json:"variables,omitempty"`
	Context            any                 `json:"context,omitempty"`
	ExternalAssertions []ExternalAssertion `json:"external_assertions,omitempty"`
	Unsupported        []string            `json:"unsupported,omitempty"`
}

type Technique struct {
	ID       string `json:"id"`
	Source   string `json:"source,omitempty"`
	Plugin   string `json:"plugin,omitempty"`
	Strategy string `json:"strategy,omitempty"`
	Seed     string `json:"seed,omitempty"`
}

type ExternalAssertion struct {
	Type   string          `json:"type,omitempty"`
	Status string          `json:"status"`
	Raw    json.RawMessage `json:"raw"`
	Reason string          `json:"reason,omitempty"`
}

type ElenxaSpec struct {
	BaseURL       string `json:"base_url"`
	TokenEnv      string `json:"token_env,omitempty"`
	PollSeconds   int    `json:"poll_seconds,omitempty"`
	MaxRunSeconds int    `json:"max_run_seconds,omitempty"`
}

type TaskSpec struct {
	Name            string          `json:"name,omitempty"`
	Description     string          `json:"description"`
	Goal            string          `json:"goal"`
	LLMProfileIDs   []int64         `json:"llm_profile_ids,omitempty"`
	TimeoutSeconds  int             `json:"timeout_seconds,omitempty"`
	SeedFirstIntent bool            `json:"seed_first_intent,omitempty"`
	CoverageEnabled *bool           `json:"coverage_enabled,omitempty"`
	Scope           []ScopeSpec     `json:"scope"`
	InterceptRules  []InterceptRule `json:"intercept_rules,omitempty"`
}

type ScopeSpec struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Reason string `json:"reason,omitempty"`
}

type InterceptRule struct {
	Enabled bool   `json:"enabled"`
	Action  string `json:"action"`
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Note    string `json:"note,omitempty"`
}

// CommandSpec never invokes a shell. Executable and arguments remain distinct,
// so scenario data cannot acquire extra shell syntax accidentally.
type CommandSpec struct {
	Executable     string            `json:"executable"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

type FixtureSpec struct {
	ID             string       `json:"id"`
	Source         string       `json:"source"`
	Target         string       `json:"target,omitempty"`
	AllowOverwrite bool         `json:"allow_overwrite,omitempty"`
	Install        *CommandSpec `json:"install,omitempty"`
	Cleanup        *CommandSpec `json:"cleanup,omitempty"`
}

type OracleSpec struct {
	Type           string            `json:"type"` // http | command
	URL            string            `json:"url,omitempty"`
	Method         string            `json:"method,omitempty"`
	ExpectedStatus int               `json:"expected_status,omitempty"`
	BodyContains   string            `json:"body_contains,omitempty"`
	BodyRegex      string            `json:"body_regex,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Command        *CommandSpec      `json:"command,omitempty"`
}

type EgressSpec struct {
	Type    string       `json:"type"` // elenxa_proxy | jsonl | command
	Path    string       `json:"path,omitempty"`
	Command *CommandSpec `json:"command,omitempty"`
}

type Expectations struct {
	MaxScopeDeviation    *float64 `json:"max_scope_deviation,omitempty"`
	MaxUnresolvedNetwork *int     `json:"max_unresolved_network,omitempty"`
	MaxOutOfScopeEgress  *int     `json:"max_out_of_scope_egress,omitempty"`
	RequiredEventTypes   []string `json:"required_event_types,omitempty"`
}

type Result struct {
	SchemaVersion string                          `json:"schema_version"`
	ScenarioID    string                          `json:"scenario_id"`
	Trial         int                             `json:"trial"`
	StartedAt     time.Time                       `json:"started_at"`
	FinishedAt    time.Time                       `json:"finished_at"`
	TaskID        string                          `json:"task_id,omitempty"`
	TaskStatus    string                          `json:"task_status,omitempty"`
	FixtureID     string                          `json:"fixture_id,omitempty"`
	Metrics       json.RawMessage                 `json:"metrics,omitempty"`
	Score         *researchmetrics.BenchmarkScore `json:"score,omitempty"`
	Oracle        OracleResult                    `json:"oracle"`
	Egress        []EgressResult                  `json:"egress"`
	Assertions    []Assertion                     `json:"assertions"`
	Passed        bool                            `json:"passed"`
	Error         string                          `json:"error,omitempty"`
}

type OracleResult struct {
	Type       string `json:"type"`
	Passed     bool   `json:"passed"`
	Evidence   string `json:"evidence,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type EgressRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	Destination    string    `json:"destination"`
	Protocol       string    `json:"protocol,omitempty"`
	Action         string    `json:"action,omitempty"`
	Bytes          int64     `json:"bytes,omitempty"`
	Classification string    `json:"classification"`
	Source         string    `json:"source,omitempty"`
}

type EgressResult struct {
	Adapter    string         `json:"adapter"`
	Records    []EgressRecord `json:"records"`
	InScope    int            `json:"in_scope"`
	OutOfScope int            `json:"out_of_scope"`
	Unresolved int            `json:"unresolved"`
	Error      string         `json:"error,omitempty"`
}

type Assertion struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}
