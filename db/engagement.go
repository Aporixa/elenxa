package db

import (
	"fmt"
	"strings"
)

// EngagementPolicy controls how noisily a task may interact with its targets.
// Zero-valued numeric limits are unlimited. Presets are expanded at task
// creation so a run remains reproducible if preset defaults change later.
type EngagementPolicy struct {
	Profile                  string  `json:"profile"`
	UserAgentMode            string  `json:"user_agent_mode"`
	UserAgent                string  `json:"user_agent,omitempty"`
	HTTPRequestsPerSecond    float64 `json:"http_requests_per_second"`
	HTTPBurst                int     `json:"http_burst"`
	HTTPMaxConcurrentPerHost int     `json:"http_max_concurrent_per_host"`
	HTTPMaxRequests          int     `json:"http_max_requests"`
	HTTPMinDelayMS           int     `json:"http_min_delay_ms"`
	ScannerMaxRate           int     `json:"scanner_max_rate"`
	ScannerMaxConcurrency    int     `json:"scanner_max_concurrency"`
}

const BrowserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func DefaultEngagementPolicy() EngagementPolicy { return EngagementPolicyForProfile("lab") }

func EngagementPolicyForProfile(profile string) EngagementPolicy {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "quiet":
		return EngagementPolicy{Profile: "quiet", UserAgentMode: "preserve", HTTPRequestsPerSecond: 2, HTTPBurst: 2, HTTPMaxConcurrentPerHost: 2, HTTPMinDelayMS: 200, ScannerMaxRate: 5, ScannerMaxConcurrency: 2}
	case "balanced":
		return EngagementPolicy{Profile: "balanced", UserAgentMode: "preserve", HTTPRequestsPerSecond: 10, HTTPBurst: 20, HTTPMaxConcurrentPerHost: 8, ScannerMaxRate: 50, ScannerMaxConcurrency: 20}
	default:
		return EngagementPolicy{Profile: "lab", UserAgentMode: "preserve"}
	}
}

// NormalizeEngagementPolicy validates a policy and expands named presets. A
// custom policy retains all explicit fields; a preset only accepts UA overrides.
func NormalizeEngagementPolicy(in EngagementPolicy) (EngagementPolicy, error) {
	profile := strings.ToLower(strings.TrimSpace(in.Profile))
	if profile == "" {
		profile = "lab"
	}
	var out EngagementPolicy
	switch profile {
	case "lab", "balanced", "quiet":
		out = EngagementPolicyForProfile(profile)
		if strings.TrimSpace(in.UserAgentMode) != "" {
			out.UserAgentMode = in.UserAgentMode
		}
		out.UserAgent = strings.TrimSpace(in.UserAgent)
	case "custom":
		out = in
		out.Profile = "custom"
		out.UserAgent = strings.TrimSpace(out.UserAgent)
	default:
		return EngagementPolicy{}, fmt.Errorf("engagement profile must be lab, balanced, quiet, or custom")
	}
	if out.UserAgentMode == "" {
		out.UserAgentMode = "preserve"
	}
	out.UserAgentMode = strings.ToLower(strings.TrimSpace(out.UserAgentMode))
	switch out.UserAgentMode {
	case "preserve":
		out.UserAgent = ""
	case "browser":
		out.UserAgent = BrowserUserAgent
	case "fixed":
		if out.UserAgent == "" {
			return EngagementPolicy{}, fmt.Errorf("a fixed user agent cannot be empty")
		}
		if len(out.UserAgent) > 512 || strings.ContainsAny(out.UserAgent, "\r\n") {
			return EngagementPolicy{}, fmt.Errorf("fixed user agent is invalid")
		}
	default:
		return EngagementPolicy{}, fmt.Errorf("user agent mode must be preserve, browser, or fixed")
	}
	if out.HTTPRequestsPerSecond < 0 || out.HTTPRequestsPerSecond > 10000 || out.HTTPBurst < 0 || out.HTTPBurst > 10000 || out.HTTPMaxConcurrentPerHost < 0 || out.HTTPMaxConcurrentPerHost > 1000 || out.HTTPMaxRequests < 0 || out.HTTPMaxRequests > 10000000 || out.HTTPMinDelayMS < 0 || out.HTTPMinDelayMS > 60000 || out.ScannerMaxRate < 0 || out.ScannerMaxRate > 1000000 || out.ScannerMaxConcurrency < 0 || out.ScannerMaxConcurrency > 10000 {
		return EngagementPolicy{}, fmt.Errorf("one or more engagement limits are outside the supported range")
	}
	if out.HTTPRequestsPerSecond > 0 && out.HTTPBurst == 0 {
		out.HTTPBurst = 1
	}
	return out, nil
}
