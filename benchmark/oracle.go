package benchmark

import (
	"context"
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
	result.Passed = true
	return result
}

func truncateEvidence(value string) string {
	const max = 4096
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max] + "…"
	}
	return value
}
