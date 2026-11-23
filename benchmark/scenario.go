package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func LoadScenario(path string) (Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	var scenario Scenario
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	if err := ValidateScenario(scenario); err != nil {
		return Scenario{}, err
	}
	if scenario.Fixture != nil && !filepath.IsAbs(scenario.Fixture.Source) {
		scenario.Fixture.Source = filepath.Join(filepath.Dir(path), scenario.Fixture.Source)
	}
	for i := range scenario.Egress {
		if scenario.Egress[i].Type == "jsonl" && !filepath.IsAbs(scenario.Egress[i].Path) {
			scenario.Egress[i].Path = filepath.Join(filepath.Dir(path), scenario.Egress[i].Path)
		}
	}
	return scenario, nil
}

func ValidateScenario(s Scenario) error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("scenario id is required")
	}
	if s.Provenance != nil {
		if strings.TrimSpace(s.Provenance.Source) == "" || strings.TrimSpace(s.Provenance.ExternalCaseID) == "" || strings.TrimSpace(s.Provenance.RenderedInput) == "" {
			return fmt.Errorf("provenance source, external_case_id, and rendered_input are required")
		}
		if !validSHA256(s.Provenance.SourceDigest) || !validSHA256(s.Provenance.ExternalCaseDigest) {
			return fmt.Errorf("provenance digests must use sha256:<64 lowercase hex characters>")
		}
		for i, assertion := range s.Provenance.ExternalAssertions {
			if assertion.Status == "" || len(assertion.Raw) == 0 || !json.Valid(assertion.Raw) {
				return fmt.Errorf("provenance external_assertions[%d] requires status and valid raw JSON", i)
			}
		}
	}
	if strings.TrimSpace(s.Elenxa.BaseURL) == "" {
		return fmt.Errorf("elenxa.base_url is required")
	}
	if strings.TrimSpace(s.Task.Description) == "" || strings.TrimSpace(s.Task.Goal) == "" {
		return fmt.Errorf("task description and goal are required")
	}
	if len(s.Task.Scope) == 0 {
		return fmt.Errorf("at least one explicit task scope entry is required")
	}
	if s.Trials < 0 || s.Trials > 1000 {
		return fmt.Errorf("trials must be between 0 and 1000")
	}
	if s.Elenxa.PollSeconds < 0 || s.Elenxa.MaxRunSeconds < 0 {
		return fmt.Errorf("elenxa polling and run limits cannot be negative")
	}
	if s.Reset != nil && strings.TrimSpace(s.Reset.Executable) == "" {
		return fmt.Errorf("reset executable is required")
	}
	if s.Fixture != nil {
		if strings.TrimSpace(s.Fixture.ID) == "" || strings.TrimSpace(s.Fixture.Source) == "" {
			return fmt.Errorf("fixture id and source are required")
		}
		if s.Fixture.Install != nil && strings.TrimSpace(s.Fixture.Install.Executable) == "" {
			return fmt.Errorf("fixture install executable is required")
		}
		if s.Fixture.Cleanup != nil && strings.TrimSpace(s.Fixture.Cleanup.Executable) == "" {
			return fmt.Errorf("fixture cleanup executable is required")
		}
	}
	if s.Oracle.Type != "http" && s.Oracle.Type != "command" {
		return fmt.Errorf("oracle.type must be http or command")
	}
	if s.Oracle.Type == "http" && strings.TrimSpace(s.Oracle.URL) == "" {
		return fmt.Errorf("http oracle URL is required")
	}
	if s.Oracle.BodyRegex != "" {
		if _, err := regexp.Compile(s.Oracle.BodyRegex); err != nil {
			return fmt.Errorf("invalid oracle body_regex: %w", err)
		}
	}
	if count := s.Oracle.JSONCount; count != nil {
		if s.Oracle.Type != "http" {
			return fmt.Errorf("oracle.json_count requires an http oracle")
		}
		if (count.Exact == nil) == (count.AtLeast == nil) {
			return fmt.Errorf("oracle.json_count requires exactly one of exact or at_least")
		}
		if count.Exact != nil && *count.Exact < 0 || count.AtLeast != nil && *count.AtLeast < 0 {
			return fmt.Errorf("oracle.json_count thresholds cannot be negative")
		}
	}
	if s.Oracle.Type == "command" && (s.Oracle.Command == nil || s.Oracle.Command.Executable == "") {
		return fmt.Errorf("command oracle executable is required")
	}
	for _, adapter := range s.Egress {
		switch adapter.Type {
		case "elenxa_proxy":
		case "jsonl":
			if adapter.Path == "" {
				return fmt.Errorf("jsonl egress path is required")
			}
		case "command":
			if adapter.Command == nil || adapter.Command.Executable == "" {
				return fmt.Errorf("command egress executable is required")
			}
		default:
			return fmt.Errorf("unsupported egress adapter %q", adapter.Type)
		}
	}
	return nil
}

var sha256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validSHA256(value string) bool { return sha256Pattern.MatchString(value) }
