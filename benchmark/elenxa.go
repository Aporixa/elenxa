package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type ElenxaClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewElenxaClient(spec ElenxaSpec) (*ElenxaClient, error) {
	base := strings.TrimRight(strings.TrimSpace(spec.BaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Elenxa base URL")
	}
	tokenEnv := spec.TokenEnv
	if tokenEnv == "" {
		tokenEnv = "ELENXA_BENCH_TOKEN"
	}
	token := strings.TrimSpace(os.Getenv(tokenEnv))
	if token == "" {
		return nil, fmt.Errorf("Elenxa bearer token is required in %s", tokenEnv)
	}
	return &ElenxaClient{baseURL: base, token: token, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

type taskState struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (c *ElenxaClient) CreateTask(ctx context.Context, spec TaskSpec) (string, error) {
	body := map[string]any{
		"name": spec.Name, "description": spec.Description, "goal": spec.Goal,
		"llm_profile_ids": spec.LLMProfileIDs, "timeout_seconds": spec.TimeoutSeconds,
		"seed_first_intent": spec.SeedFirstIntent, "intercept_rules": spec.InterceptRules,
		"scope": spec.Scope,
	}
	if spec.CoverageEnabled != nil {
		body["coverage_enabled"] = *spec.CoverageEnabled
	}
	var created taskState
	if err := c.doJSON(ctx, http.MethodPost, "/api/tasks", body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("Elenxa returned an empty task id")
	}
	return created.ID, nil
}

func (c *ElenxaClient) WaitTask(ctx context.Context, taskID string, poll time.Duration) (string, error) {
	if poll <= 0 {
		poll = 5 * time.Second
	}
	for {
		var state taskState
		if err := c.doJSON(ctx, http.MethodGet, "/api/tasks/"+url.PathEscape(taskID), nil, &state); err != nil {
			return "", err
		}
		switch state.Status {
		case "done", "failed", "timeout":
			return state.Status, nil
		}
		select {
		case <-ctx.Done():
			return state.Status, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// PauseTask is best-effort cleanup for a benchmark deadline. It prevents a
// timed-out trial from continuing to spend model calls after the runner exits.
func (c *ElenxaClient) PauseTask(ctx context.Context, taskID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/tasks/"+url.PathEscape(taskID)+"/control", map[string]string{"action": "pause"}, nil)
}

func (c *ElenxaClient) ResearchMetrics(ctx context.Context, taskID string) (json.RawMessage, error) {
	return c.rawJSON(ctx, http.MethodGet, "/api/tasks/"+url.PathEscape(taskID)+"/research-metrics", nil)
}

func (c *ElenxaClient) ProxyTraffic(ctx context.Context, started, finished time.Time) ([]EgressRecord, error) {
	var records []EgressRecord
	for page := 0; page < 20; page++ {
		var response struct {
			Exchanges []struct {
				TS     time.Time `json:"ts"`
				Host   string    `json:"host"`
				Method string    `json:"method"`
				URL    string    `json:"url"`
			} `json:"exchanges"`
		}
		path := fmt.Sprintf("/api/traffic?page=%d&size=500&sort=ts&order=desc", page)
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		if len(response.Exchanges) == 0 {
			break
		}
		older := false
		for _, exchange := range response.Exchanges {
			if exchange.TS.Before(started) {
				older = true
				continue
			}
			if exchange.TS.After(finished) {
				continue
			}
			records = append(records, EgressRecord{
				Timestamp: exchange.TS, Destination: exchange.Host, Protocol: "http",
				Action: "observed", Source: "elenxa_proxy:" + exchange.Method + " " + exchange.URL,
			})
		}
		if older {
			break
		}
	}
	return records, nil
}

func (c *ElenxaClient) doJSON(ctx context.Context, method, path string, body, target any) error {
	raw, err := c.rawJSON(ctx, method, path, body)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode Elenxa response: %w", err)
	}
	return nil
}

func (c *ElenxaClient) rawJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Elenxa %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}
