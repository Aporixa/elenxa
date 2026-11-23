package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func evaluateOracle(ctx context.Context, spec OracleSpec, env map[string]string) OracleResult {
	result := OracleResult{Type: spec.Type}
	if spec.Type == "command" {
		out, err := runCommand(ctx, *spec.Command, env)
		result.Evidence = truncateEvidence(string(out))
		result.Passed = err == nil
		if err != nil {
			result.Error = err.Error()
		}
		return result
	}
	method := spec.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, spec.URL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for key, value := range spec.Headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	result.StatusCode = resp.StatusCode
	result.Evidence = truncateEvidence(string(body))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	wantStatus := spec.ExpectedStatus
	if wantStatus == 0 {
		wantStatus = http.StatusOK
	}
	if resp.StatusCode != wantStatus {
		result.Error = fmt.Sprintf("status %d, want %d", resp.StatusCode, wantStatus)
		return result
	}
	if spec.BodyContains != "" && !strings.Contains(string(body), spec.BodyContains) {
		result.Error = "response did not contain required text"
		return result
	}
	if spec.BodyRegex != "" {
		pattern, compileErr := regexp.Compile(spec.BodyRegex)
		if compileErr != nil {
			result.Error = "invalid oracle body_regex: " + compileErr.Error()
			return result
		}
		if !pattern.Match(body) {
			result.Error = "response did not match required expression"
			return result
		}
	}
	if spec.JSONCount != nil {
		count, countErr := evaluateJSONCount(body, *spec.JSONCount)
		result.MatchCount = &count
		if countErr != nil {
			result.Error = countErr.Error()
			return result
		}
	}
	result.Passed = true
	return result
}

func evaluateJSONCount(body []byte, spec JSONCountSpec) (int, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return 0, fmt.Errorf("oracle response is not valid JSON: %w", err)
	}
	for _, part := range strings.Split(spec.Path, ".") {
		if part == "" {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("oracle json_count path %q does not resolve to an object", spec.Path)
		}
		var found bool
		value, found = object[part]
		if !found {
			return 0, fmt.Errorf("oracle json_count path %q is missing", spec.Path)
		}
	}
	items, ok := value.([]any)
	if !ok {
		return 0, fmt.Errorf("oracle json_count path %q does not resolve to an array", spec.Path)
	}
	count := 0
	for _, item := range items {
		if matchesJSONFields(item, spec.Match) {
			count++
		}
	}
	switch {
	case spec.Exact != nil && count != *spec.Exact:
		return count, fmt.Errorf("JSON match count %d, want exactly %d", count, *spec.Exact)
	case spec.AtLeast != nil && count < *spec.AtLeast:
		return count, fmt.Errorf("JSON match count %d, want at least %d", count, *spec.AtLeast)
	default:
		return count, nil
	}
}

func matchesJSONFields(value any, match map[string]any) bool {
	if len(match) == 0 {
		return true
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, expected := range match {
		actual, exists := object[key]
		if !exists || !jsonValuesEqual(actual, expected) {
			return false
		}
	}
	return true
}

func jsonValuesEqual(actual, expected any) bool {
	a, aErr := json.Marshal(actual)
	b, bErr := json.Marshal(expected)
	return aErr == nil && bErr == nil && string(a) == string(b)
}

func truncateEvidence(value string) string {
	const max = 4096
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max] + "…"
	}
	return value
}
