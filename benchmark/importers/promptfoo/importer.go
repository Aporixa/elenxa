// Package promptfoo converts frozen Promptfoo cases into native Elenxa
// benchmark scenarios. Import is offline and never invokes a provider.
package promptfoo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Aporixa/elenxa/benchmark"
	"gopkg.in/yaml.v3"
)

type Options struct {
	SourcePath    string
	SourceVersion string
}

type ImportedCase struct {
	Scenario    benchmark.Scenario `json:"scenario"`
	Diagnostics []string           `json:"diagnostics,omitempty"`
}

type prompt struct {
	ID   string
	Text string
}

var templateRE = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

func Import(raw []byte, template benchmark.Scenario, opts Options) ([]ImportedCase, error) {
	if err := benchmark.ValidateScenario(template); err != nil {
		return nil, fmt.Errorf("invalid lab template: %w", err)
	}
	root, err := decode(raw)
	if err != nil {
		return nil, err
	}
	prompts, err := parsePrompts(root["prompts"])
	if err != nil {
		return nil, err
	}
	tests, ok := slice(root["tests"])
	if !ok || len(tests) == 0 {
		return nil, fmt.Errorf("Promptfoo input has no concrete tests; run generation externally and import the frozen suite")
	}
	if len(prompts) == 0 {
		prompts = []prompt{{ID: "case-input"}}
	}
	suiteID := stringValue(root["description"])
	if suiteID == "" {
		suiteID = stringValue(root["id"])
	}
	sourceHash := digest(raw)
	var out []ImportedCase
	for testIndex, rawTest := range tests {
		test, ok := object(rawTest)
		if !ok {
			return nil, fmt.Errorf("tests[%d] must be an object", testIndex)
		}
		vars, _ := object(test["vars"])
		caseInput := firstString(test, "prompt", "input")
		caseID := firstString(test, "id", "description")
		if caseID == "" {
			caseID = fmt.Sprintf("case-%03d", testIndex+1)
		}
		caseRaw, _ := json.Marshal(test)
		casePrompts := prompts
		if caseInput != "" {
			casePrompts = []prompt{{ID: "case-input", Text: caseInput}}
		}
		for promptIndex, basePrompt := range casePrompts {
			text := caseInput
			promptID := basePrompt.ID
			if text == "" {
				text = basePrompt.Text
			}
			if strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("tests[%d] has no concrete prompt/input and prompts[%d] is not inline text", testIndex, promptIndex)
			}
			if isFileBackedPrompt(text) {
				return nil, fmt.Errorf("tests[%d] prompt is file-backed; freeze it to inline text before import", testIndex)
			}
			rendered, unresolved := render(text, vars)
			if len(unresolved) > 0 {
				return nil, fmt.Errorf("tests[%d] has unresolved variables: %s", testIndex, strings.Join(unresolved, ", "))
			}
			idSuffix := caseID
			if len(casePrompts) > 1 {
				idSuffix += "-" + promptID
			}
			scenario := template
			identity := fmt.Sprintf("test=%d;prompt=%d;case=%s;rendered=%s", testIndex, promptIndex, digest(caseRaw), rendered)
			scenario.ID = stableID(template.ID, idSuffix, identity)
			scenario.Description = strings.TrimSpace("Imported Promptfoo case " + caseID + ". " + template.Description)
			scenario.Task.Description = injectDescription(template.Task.Description, rendered, caseID)
			scenario.Task.Goal = replaceCaseFields(template.Task.Goal, rendered, caseID)
			assertions, diagnostics := parseAssertions(test["assert"])
			metadata, _ := object(test["metadata"])
			technique := techniqueFrom(metadata)
			unsupported := append([]string(nil), diagnostics...)
			for _, key := range unknownTestKeys(test) {
				unsupported = append(unsupported, "unmapped test field: "+key)
			}
			contextValue := test["context"]
			if contextValue == nil {
				contextValue = vars["context"]
			}
			provenance := &benchmark.Provenance{
				Source: "promptfoo", SourceReference: opts.SourcePath, SourceVersion: opts.SourceVersion, SourceDigest: sourceHash,
				ExternalSuiteID: suiteID, ExternalCaseID: caseID,
				ExternalCaseDigest: digest(caseRaw), PromptID: promptID,
				RenderedInput: rendered, Variables: vars, Context: contextValue,
				ExternalAssertions: assertions, Unsupported: unsupported,
			}
			scenario.Provenance = provenance
			if technique.ID != "" || technique.Plugin != "" || technique.Strategy != "" {
				scenario.Techniques = append(append([]benchmark.Technique(nil), template.Techniques...), technique)
			}
			out = append(out, ImportedCase{Scenario: scenario, Diagnostics: unsupported})
		}
	}
	return out, nil
}

func decode(raw []byte) (map[string]any, error) {
	var value any
	if err := yaml.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode Promptfoo input: %w", err)
	}
	normalized := normalize(value)
	root, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Promptfoo input must be an object")
	}
	return root, nil
}

func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalize(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalize(item)
		}
		return out
	case []any:
		for i := range v {
			v[i] = normalize(v[i])
		}
		return v
	default:
		return value
	}
}

func parsePrompts(value any) ([]prompt, error) {
	items, ok := slice(value)
	if !ok && value != nil {
		items = []any{value}
	}
	var out []prompt
	for i, item := range items {
		switch v := item.(type) {
		case string:
			if isFileBackedPrompt(v) {
				return nil, fmt.Errorf("prompts[%d] is file-backed; freeze it to inline text before import", i)
			}
			out = append(out, prompt{ID: fmt.Sprintf("prompt-%d", i+1), Text: v})
		case map[string]any:
			text := firstString(v, "raw", "prompt", "text")
			if text == "" {
				return nil, fmt.Errorf("prompts[%d] is dynamic or file-backed; freeze it to inline raw text before import", i)
			}
			id := firstString(v, "id", "label")
			if id == "" {
				id = fmt.Sprintf("prompt-%d", i+1)
			}
			out = append(out, prompt{ID: id, Text: text})
		default:
			return nil, fmt.Errorf("prompts[%d] must be inline text or an object with raw/text", i)
		}
	}
	return out, nil
}

func render(text string, vars map[string]any) (string, []string) {
	missing := map[string]bool{}
	out := templateRE.ReplaceAllStringFunc(text, func(match string) string {
		parts := templateRE.FindStringSubmatch(match)
		value, ok := lookup(vars, parts[1])
		if !ok {
			missing[parts[1]] = true
			return match
		}
		return fmt.Sprint(value)
	})
	var unresolved []string
	for key := range missing {
		unresolved = append(unresolved, key)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
		unresolved = append(unresolved, "unsupported template expression")
	}
	sort.Strings(unresolved)
	return out, unresolved
}

func isFileBackedPrompt(text string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "file://")
}

func lookup(values map[string]any, path string) (any, bool) {
	var current any = values
	for _, part := range strings.Split(path, ".") {
		row, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = row[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func replaceCaseFields(template, rendered, caseID string) string {
	replaced := strings.ReplaceAll(template, "{{promptfoo_input}}", rendered)
	replaced = strings.ReplaceAll(replaced, "{{promptfoo_case_id}}", caseID)
	return strings.TrimSpace(replaced)
}

func injectDescription(template, rendered, caseID string) string {
	hasInputPlaceholder := strings.Contains(template, "{{promptfoo_input}}")
	replaced := replaceCaseFields(template, rendered, caseID)
	if !hasInputPlaceholder && !strings.Contains(template, rendered) {
		replaced = strings.TrimSpace(template) + "\n\nFrozen external case input:\n" + rendered
	}
	return strings.TrimSpace(replaced)
}

func parseAssertions(value any) ([]benchmark.ExternalAssertion, []string) {
	items, ok := slice(value)
	if !ok && value != nil {
		items = []any{value}
	}
	assertions := make([]benchmark.ExternalAssertion, 0, len(items))
	var diagnostics []string
	for i, item := range items {
		raw, _ := json.Marshal(item)
		assertionType := ""
		if row, ok := object(item); ok {
			assertionType = stringValue(row["type"])
		}
		reason := "prompt-level assertions are not automatically equivalent to the lab oracle"
		assertions = append(assertions, benchmark.ExternalAssertion{Type: assertionType, Status: "preserved_external", Raw: raw, Reason: reason})
		diagnostics = append(diagnostics, fmt.Sprintf("assert[%d] preserved_external: %s", i, reason))
	}
	return assertions, diagnostics
}

func techniqueFrom(metadata map[string]any) benchmark.Technique {
	return benchmark.Technique{
		ID: firstString(metadata, "technique", "technique_id", "framework_id"), Source: "promptfoo",
		Plugin:   firstString(metadata, "plugin", "pluginId", "plugin_id"),
		Strategy: firstString(metadata, "strategy", "strategyId", "strategy_id"),
		Seed:     valueString(metadata["seed"]),
	}
}

func unknownTestKeys(test map[string]any) []string {
	known := map[string]bool{"id": true, "description": true, "vars": true, "prompt": true, "input": true, "context": true, "assert": true, "metadata": true}
	var out []string
	for key := range test {
		if !known[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func stableID(prefix, label, rendered string) string {
	label = strings.ToLower(label)
	var clean strings.Builder
	for _, r := range label {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			clean.WriteRune(r)
		} else if clean.Len() > 0 && !strings.HasSuffix(clean.String(), "-") {
			clean.WriteByte('-')
		}
	}
	slug := strings.Trim(clean.String(), "-")
	if slug == "" {
		slug = "case"
	}
	if len(slug) > 48 {
		slug = slug[:48]
	}
	hash := sha256.Sum256([]byte(rendered))
	return strings.Trim(prefix, "-") + "-" + slug + "-" + hex.EncodeToString(hash[:4])
}

func digest(raw []byte) string {
	hash := sha256.Sum256(bytes.TrimSpace(raw))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func object(value any) (map[string]any, bool) { row, ok := value.(map[string]any); return row, ok }
func slice(value any) ([]any, bool)           { rows, ok := value.([]any); return rows, ok }
func stringValue(value any) string            { text, _ := value.(string); return strings.TrimSpace(text) }
func valueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}
func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(row[key]); value != "" {
			return value
		}
	}
	return ""
}
