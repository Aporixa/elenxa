package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Aporixa/elenxa/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// engagementTool keeps scanner fan-out inside the task policy. It rejects an
// unbounded scanner invocation with a concrete correction; ordinary shell
// commands and lab-profile runs are unchanged.
type engagementTool struct {
	actool.CoreTool
	policy  db.EngagementPolicy
	onEvent func(action, detail string)
}

var scannerCommand = regexp.MustCompile(`(?i)(?:^|[;&|]\s*|\bsudo\s+)(nmap|masscan|ffuf|gobuster|feroxbuster|nuclei|sqlmap|nikto|dirsearch|wfuzz)\b`)

func applyEngagementPolicy(tools []actool.CoreTool, policy db.EngagementPolicy, onEvent func(string, string)) []actool.CoreTool {
	if policy.ScannerMaxRate <= 0 && policy.ScannerMaxConcurrency <= 0 {
		return tools
	}
	out := append([]actool.CoreTool(nil), tools...)
	for i, tool := range out {
		if tool.Name() == "Bash" || tool.Name() == "shell_open" {
			out[i] = &engagementTool{CoreTool: tool, policy: policy, onEvent: onEvent}
		}
	}
	return out
}

func (t *engagementTool) CheckPermissions(ctx context.Context, input json.RawMessage, pc permission.Context) permission.Decision {
	return t.CoreTool.CheckPermissions(ctx, input, pc)
}

func (t *engagementTool) Call(ctx context.Context, input json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &args) != nil || strings.TrimSpace(args.Command) == "" {
		return t.CoreTool.Call(ctx, input, tc)
	}
	m := scannerCommand.FindStringSubmatch(args.Command)
	if len(m) < 2 {
		return t.CoreTool.Call(ctx, input, tc)
	}
	tool := strings.ToLower(m[1])
	missing := scannerLimitGuidance(tool, args.Command, t.policy)
	if missing == "" {
		return t.CoreTool.Call(ctx, input, tc)
	}
	if t.onEvent != nil {
		t.onEvent("scanner_block", tool+": "+missing)
	}
	return actool.Errorf(fmt.Sprintf("blocked by engagement policy: %s. Retry with %s", missing, scannerFlags(tool, t.policy))), nil
}

func scannerLimitGuidance(tool, command string, p db.EngagementPolicy) string {
	rate, hasRate, concurrency, hasConcurrency := 0, false, 0, false
	switch tool {
	case "nmap":
		rate, hasRate = commandFlagInt(command, "--max-rate")
		concurrency, hasConcurrency = commandFlagInt(command, "--max-parallelism")
	case "masscan":
		rate, hasRate = commandFlagInt(command, "--rate")
		hasConcurrency = true
	case "ffuf":
		rate, hasRate = commandFlagInt(command, "-rate")
		concurrency, hasConcurrency = commandFlagInt(command, "-t")
	case "nuclei":
		rate, hasRate = commandFlagInt(command, "-rl")
		concurrency, hasConcurrency = commandFlagInt(command, "-c")
	case "feroxbuster":
		rate, hasRate = commandFlagInt(command, "--rate-limit")
		concurrency, hasConcurrency = commandFlagInt(command, "--threads")
	case "gobuster", "sqlmap", "dirsearch", "wfuzz":
		// These tools do not share a reliable requests/second flag. Their HTTP
		// traffic remains bounded by the proxy, while thread fan-out is bounded here.
		hasRate = true
		concurrency, hasConcurrency = commandFlagInt(command, "--threads")
		if !hasConcurrency {
			concurrency, hasConcurrency = commandFlagInt(command, "-t")
		}
	case "nikto":
		// Nikto has no stable rate/thread flag; the HTTP proxy is authoritative.
		hasRate, hasConcurrency = true, true
	}
	var missing []string
	if p.ScannerMaxRate > 0 && !hasRate {
		missing = append(missing, fmt.Sprintf("an explicit rate limit no greater than %d requests/second", p.ScannerMaxRate))
	} else if p.ScannerMaxRate > 0 && rate > p.ScannerMaxRate {
		missing = append(missing, fmt.Sprintf("a rate limit no greater than %d requests/second (got %d)", p.ScannerMaxRate, rate))
	}
	if p.ScannerMaxConcurrency > 0 && !hasConcurrency {
		missing = append(missing, fmt.Sprintf("an explicit concurrency limit no greater than %d", p.ScannerMaxConcurrency))
	} else if p.ScannerMaxConcurrency > 0 && concurrency > p.ScannerMaxConcurrency {
		missing = append(missing, fmt.Sprintf("a concurrency limit no greater than %d (got %d)", p.ScannerMaxConcurrency, concurrency))
	}
	return strings.Join(missing, " and ")
}

func commandFlagInt(command, flag string) (int, bool) {
	re := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(flag) + `(?:=|\s+)(\d+)(?:\s|$)`)
	m := re.FindStringSubmatch(command)
	if len(m) != 2 {
		return 0, false
	}
	var value int
	_, err := fmt.Sscanf(m[1], "%d", &value)
	return value, err == nil
}

func scannerFlags(tool string, p db.EngagementPolicy) string {
	switch tool {
	case "nmap":
		return fmt.Sprintf("--max-rate %d --max-parallelism %d", p.ScannerMaxRate, p.ScannerMaxConcurrency)
	case "masscan":
		return fmt.Sprintf("--rate %d", p.ScannerMaxRate)
	case "ffuf":
		return fmt.Sprintf("-rate %d -t %d", p.ScannerMaxRate, p.ScannerMaxConcurrency)
	case "nuclei":
		return fmt.Sprintf("-rl %d -c %d", p.ScannerMaxRate, p.ScannerMaxConcurrency)
	case "feroxbuster":
		return fmt.Sprintf("--rate-limit %d --threads %d", p.ScannerMaxRate, p.ScannerMaxConcurrency)
	case "gobuster", "sqlmap", "dirsearch", "wfuzz":
		return fmt.Sprintf("--threads %d (and use the tool's delay option to stay at or below %d requests/second)", p.ScannerMaxConcurrency, p.ScannerMaxRate)
	default:
		return "a bounded-rate equivalent or use a different bounded tool"
	}
}

func engagementPrompt(p db.EngagementPolicy) string {
	if p.Profile == "" {
		p = db.DefaultEngagementPolicy()
	}
	return fmt.Sprintf("\n\n[Engagement traffic policy]\nProfile: %s. HTTP rate: %.2f requests/second; burst: %d; per-host concurrency: %d; total request cap: %d. Scanner maximum rate: %d; scanner maximum concurrency: %d. User-Agent mode: %s. These controls are enforced by the proxy and shell-tool middleware. Do not attempt to bypass them.", p.Profile, p.HTTPRequestsPerSecond, p.HTTPBurst, p.HTTPMaxConcurrentPerHost, p.HTTPMaxRequests, p.ScannerMaxRate, p.ScannerMaxConcurrency, p.UserAgentMode)
}
