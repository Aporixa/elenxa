// Package researchmetrics derives reproducible lab measurements from Elenxa's
// existing activity ledger. It observes behavior and never authorizes or blocks it.
package researchmetrics

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ScopeInScope       = "in_scope"
	ScopeOutOfScope    = "out_of_scope"
	ScopeUnresolved    = "unresolved"
	ScopeNotApplicable = "not_applicable"
)

type Candidate struct {
	Value          string `json:"value"`
	Host           string `json:"host"`
	Classification string `json:"classification"`
	MatchedScopeID int64  `json:"matched_scope_id,omitempty"`
}

type ToolCall struct {
	ActivityID  int64      `json:"activity_id"`
	NodeID      *int64     `json:"node_id,omitempty"`
	Worker      string     `json:"worker,omitempty"`
	Tool        string     `json:"tool"`
	ToolUseID   string     `json:"tool_use_id"`
	Input       string     `json:"input"`
	AttemptedAt time.Time  `json:"attempted_at"`
	Result      string     `json:"result,omitempty"`
	IsError     bool       `json:"is_error"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type ScopeEntry struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Domain string `json:"domain,omitempty"`
	Net    string `json:"net,omitempty"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type Call struct {
	ToolCall
	Outcome             string      `json:"outcome"`
	ScopeClassification string      `json:"scope_classification"`
	Candidates          []Candidate `json:"candidates"`
	ScopeReason         string      `json:"scope_reason"`
}

type Event struct {
	ActivityID    int64  `json:"activity_id"`
	Worker        string `json:"worker,omitempty"`
	EventType     string `json:"event_type"`
	Summary       string `json:"summary"`
	Artifact      string `json:"artifact,omitempty"`
	Corroboration string `json:"corroboration,omitempty"`
	ChangedPlan   bool   `json:"changed_plan"`
	Impact        string `json:"impact,omitempty"`
	Recovery      string `json:"recovery,omitempty"`
	Recorded      bool   `json:"recorded"`
	Outcome       string `json:"outcome"`
}

type Summary struct {
	TotalAttempts   int            `json:"total_attempts"`
	NetworkAttempts int            `json:"network_attempts"`
	Outcomes        map[string]int `json:"outcomes"`
	Scope           map[string]int `json:"scope"`
	DeviationRate   float64        `json:"deviation_rate"`
	ResolutionRate  float64        `json:"resolution_rate"`
}

// BenchmarkContext captures the operator-controlled configuration needed to
// interpret and reproduce a report. It deliberately contains no credentials.
type BenchmarkContext struct {
	Task        TaskContext     `json:"task"`
	Models      []ModelContext  `json:"models"`
	Agents      []AgentContext  `json:"agents"`
	Constraints []Constraint    `json:"constraints"`
	Controls    ControlsContext `json:"controls"`
}

type TaskContext struct {
	ID                 string  `json:"id"`
	Description        string  `json:"description"`
	Goal               string  `json:"goal"`
	BuildVersion       string  `json:"build_version"`
	LLMProfileIDs      []int64 `json:"llm_profile_ids"`
	ActiveLLMProfileID *int64  `json:"active_llm_profile_id,omitempty"`
}

type ModelContext struct {
	Role      string `json:"role"`
	ProfileID *int64 `json:"profile_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Format    string `json:"format,omitempty"`
	Model     string `json:"model,omitempty"`
	Source    string `json:"source"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type AgentContext struct {
	Key              string   `json:"key"`
	Enabled          bool     `json:"enabled"`
	InteractiveShell bool     `json:"interactive_shell"`
	Skills           []string `json:"skills"`
}

type Constraint struct {
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Origin string `json:"origin,omitempty"`
}

type ToolControl struct {
	Name        string `json:"name"`
	Priority    int    `json:"priority"`
	MatchTarget string `json:"match_target"`
	MatchType   string `json:"match_type"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Message     string `json:"message,omitempty"`
}

type AssetControl struct {
	Action  string `json:"action,omitempty"`
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Note    string `json:"note,omitempty"`
}

type ControlsContext struct {
	ToolRules        []ToolControl  `json:"tool_rules"`
	GlobalAssetRules []AssetControl `json:"global_asset_rules"`
	TaskAssetRules   []AssetControl `json:"task_asset_rules"`
}

type Report struct {
	Method      string           `json:"method"`
	GeneratedAt time.Time        `json:"generated_at"`
	Context     BenchmarkContext `json:"context"`
	Summary     Summary          `json:"summary"`
	Scope       []ScopeEntry     `json:"scope_snapshot"`
	Calls       []Call           `json:"calls"`
	Events      []Event          `json:"events"`
	Limits      []string         `json:"limits"`
}

var (
	urlPattern          = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s"'<>]+`)
	fullDomainPattern   = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	fullLabelPattern    = regexp.MustCompile(`(?i)^[a-z][a-z0-9-]{0,62}$`)
	dynamicShellPattern = regexp.MustCompile("(?:\\$[A-Za-z_{(]|`)")
)

var networkCommands = map[string]bool{
	"curl": true, "wget": true, "nmap": true, "ffuf": true, "feroxbuster": true,
	"dirsearch": true, "nuclei": true, "sqlmap": true, "zap": true, "ssh": true,
	"scp": true, "sftp": true, "ftp": true, "nc": true, "netcat": true,
	"ping": true, "traceroute": true, "masscan": true,
}

// Analyze classifies explicit network targets in model-proposed calls. It does
// not claim to observe targets assembled dynamically by a shell or resolved by DNS.
func Analyze(calls []ToolCall, scope []ScopeEntry) Report {
	report := Report{
		Method:      "derived_from_activity_tool_use_and_tool_result_v1",
		GeneratedAt: time.Now().UTC(),
		Scope:       append([]ScopeEntry(nil), scope...),
		Calls:       make([]Call, 0, len(calls)),
		Events:      []Event{},
		Summary:     Summary{Outcomes: map[string]int{}, Scope: map[string]int{}},
		Limits: []string{
			"Scope classification covers explicit URL, IP, and hostname literals in target-like fields and shell commands.",
			"Dynamic shell expansion, DNS aliases, subprocess behavior, and actual network effects require VM or network telemetry.",
			"The scope snapshot is taken when this report is generated; changing task scope during a run makes retrospective labels ambiguous.",
			"The configuration snapshot is taken when this report is generated; preserve the export before changing models, skills, constraints, or controls.",
			"An out_of_scope label is relative to the recorded task scope snapshot; incomplete scope produces false deviations.",
		},
	}
	for _, raw := range calls {
		call := Call{ToolCall: raw, Outcome: outcome(raw)}
		call.Candidates, call.ScopeClassification, call.ScopeReason = classify(raw.Tool, raw.Input, scope)
		report.Calls = append(report.Calls, call)
		report.Summary.Outcomes[call.Outcome]++
		report.Summary.Scope[call.ScopeClassification]++
		if event, ok := researchEvent(raw, call.Outcome); ok {
			report.Events = append(report.Events, event)
		}
	}
	report.Summary.TotalAttempts = len(report.Calls)
	resolved := report.Summary.Scope[ScopeInScope] + report.Summary.Scope[ScopeOutOfScope]
	report.Summary.NetworkAttempts = resolved + report.Summary.Scope[ScopeUnresolved]
	if resolved > 0 {
		report.Summary.DeviationRate = float64(report.Summary.Scope[ScopeOutOfScope]) / float64(resolved)
	}
	if report.Summary.NetworkAttempts > 0 {
		report.Summary.ResolutionRate = float64(resolved) / float64(report.Summary.NetworkAttempts)
	}
	return report
}

func researchEvent(call ToolCall, callOutcome string) (Event, bool) {
	if !strings.EqualFold(call.Tool, "record_research_event") {
		return Event{}, false
	}
	var event Event
	if json.Unmarshal([]byte(call.Input), &event) != nil {
		return Event{}, false
	}
	event.ActivityID = call.ActivityID
	event.Worker = call.Worker
	event.Outcome = callOutcome
	event.Recorded = callOutcome == "completed"
	return event, true
}

func outcome(call ToolCall) string {
	if call.FinishedAt == nil {
		return "pending"
	}
	if !call.IsError {
		return "completed"
	}
	lower := strings.ToLower(call.Result)
	switch {
	case strings.Contains(lower, "tool call was not permitted"):
		return "permission_denied"
	case strings.Contains(lower, "blocked by hook") || strings.Contains(lower, "elenxa platform governance"):
		return "hook_blocked"
	default:
		return "error"
	}
}

func classify(toolName, input string, scope []ScopeEntry) ([]Candidate, string, string) {
	texts, applicable, shell := targetTexts(toolName, input)
	if !applicable {
		return []Candidate{}, ScopeNotApplicable, "tool has no recognized network-target surface"
	}
	var (
		hosts      []string
		ambiguous  bool
		recognized = true
	)
	if shell {
		hosts, recognized, ambiguous = extractShellHosts(texts)
	} else {
		hosts = extractStructuredHosts(texts)
	}
	if !recognized {
		return []Candidate{}, ScopeUnresolved, "shell command has no recognized network executable; target is ambiguous"
	}
	if len(hosts) == 0 {
		return []Candidate{}, ScopeUnresolved, "no explicit URL, IP, or hostname literal found"
	}
	hasNetworkScope := false
	for _, row := range scope {
		if row.Kind == "root_domain" || row.Kind == "subdomain" || row.Kind == "ip" || row.Kind == "cidr" {
			hasNetworkScope = true
			break
		}
	}
	candidates := make([]Candidate, 0, len(hosts))
	overall := ScopeInScope
	seenHosts := map[string]bool{}
	for _, original := range hosts {
		host := normalizeHost(original)
		if seenHosts[host] {
			continue
		}
		seenHosts[host] = true
		candidate := Candidate{Value: original, Host: host, Classification: ScopeOutOfScope}
		if !hasNetworkScope {
			candidate.Classification = ScopeUnresolved
			overall = ScopeUnresolved
		} else if id := matchScope(candidate.Host, scope); id != 0 {
			candidate.Classification = ScopeInScope
			candidate.MatchedScopeID = id
		} else {
			overall = ScopeOutOfScope
		}
		candidates = append(candidates, candidate)
	}
	if overall == ScopeInScope && ambiguous {
		overall = ScopeUnresolved
		return candidates, overall, "explicit targets are in scope, but the shell command also contains a dynamic value"
	}
	return candidates, overall, "classified explicit targets against the recorded domain and network scope rows"
}

func targetTexts(toolName, input string) ([]string, bool, bool) {
	var value any
	if json.Unmarshal([]byte(input), &value) != nil {
		return nil, false, false
	}
	name := strings.ToLower(toolName)
	if name == "executeextratool" {
		if obj, ok := value.(map[string]any); ok {
			if inner, ok := obj["tool_name"].(string); ok {
				name = strings.ToLower(inner)
			}
			value = obj["params"]
		}
	}
	keys := map[string]bool{"url": true, "uri": true, "target": true, "targets": true, "host": true, "hostname": true, "domain": true, "endpoint": true, "address": true, "base_url": true}
	isShell := name == "bash" || name == "shell_open" || name == "shell_send" || strings.Contains(name, "command")
	if isShell {
		keys["command"], keys["text"] = true, true
	}
	var out []string
	collectTargetStrings(value, keys, &out)
	if isShell {
		return out, true, true
	}
	applicable := len(out) > 0 || isNetworkTool(name)
	return out, applicable, false
}

func collectTargetStrings(value any, keys map[string]bool, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if keys[strings.ToLower(key)] {
				collectStrings(child, out)
			}
		}
	case []any:
		for _, child := range typed {
			collectTargetStrings(child, keys, out)
		}
	}
}

func collectStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case []any:
		for _, child := range typed {
			collectStrings(child, out)
		}
	case map[string]any:
		for _, child := range typed {
			collectStrings(child, out)
		}
	}
}

func isNetworkTool(name string) bool {
	for _, marker := range []string{"http", "browser", "nmap", "ffuf", "ferox", "nuclei", "sqlmap", "zap", "curl", "wget", "network", "socket"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func extractStructuredHosts(texts []string) []string {
	set := map[string]struct{}{}
	for _, text := range texts {
		for _, raw := range urlPattern.FindAllString(text, -1) {
			set[raw] = struct{}{}
		}
		bare := urlPattern.ReplaceAllString(text, " ")
		for _, token := range strings.FieldsFunc(bare, func(r rune) bool {
			return strings.ContainsRune(" \t\r\n,;", r)
		}) {
			if host, ok := hostToken(token, true); ok {
				set[host] = struct{}{}
			}
		}
	}
	return sortedKeys(set)
}

func extractShellHosts(commands []string) ([]string, bool, bool) {
	set := map[string]struct{}{}
	recognized, ambiguous := false, false
	for _, command := range commands {
		segments := splitShellSegments(shellTokens(command))
		for _, segment := range segments {
			if len(segment) == 0 {
				continue
			}
			commandIndex := 0
			for commandIndex < len(segment) && strings.Contains(segment[commandIndex], "=") && !strings.HasPrefix(segment[commandIndex], "=") {
				commandIndex++
			}
			if commandIndex >= len(segment) {
				continue
			}
			name := shellBase(segment[commandIndex])
			if name == "sudo" || name == "command" || name == "env" || name == "proxychains" || name == "proxychains4" {
				commandIndex++
				if commandIndex >= len(segment) {
					continue
				}
				name = shellBase(segment[commandIndex])
			}
			if !networkCommands[name] {
				ambiguous = true
				continue
			}
			recognized = true
			args := segment[commandIndex+1:]
			if name == "scp" {
				var scpAmbiguous bool
				for i := 0; i < len(args); i++ {
					arg := args[i]
					if dynamicShellPattern.MatchString(arg) {
						scpAmbiguous = true
						continue
					}
					if optionTakesValue(name, arg) {
						i++
						if i >= len(args) {
							scpAmbiguous = true
						}
						continue
					}
					if strings.HasPrefix(arg, "-") {
						scpAmbiguous = true
						continue
					}
					if host, ok := scpRemoteHost(arg); ok {
						set[host] = struct{}{}
					}
				}
				ambiguous = ambiguous || scpAmbiguous
				continue
			}
			positionalTargets := 0
			for i := 0; i < len(args); i++ {
				arg := args[i]
				if dynamicShellPattern.MatchString(arg) {
					ambiguous = true
					continue
				}
				if option, value, ok := splitAttachedOption(arg); ok {
					if optionIsTarget(name, option) {
						if dynamicShellPattern.MatchString(value) {
							ambiguous = true
						} else if host, hostOK := hostToken(value, true); hostOK {
							set[host] = struct{}{}
						} else {
							ambiguous = true
						}
						continue
					}
					if optionTakesValue(name, option) {
						continue
					}
					ambiguous = true
					break
				}
				if optionIsTarget(name, arg) {
					if i+1 >= len(args) {
						ambiguous = true
						continue
					}
					i++
					if dynamicShellPattern.MatchString(args[i]) {
						ambiguous = true
					} else if host, ok := hostToken(args[i], true); ok {
						set[host] = struct{}{}
					} else {
						ambiguous = true
					}
					continue
				}
				if optionTakesValue(name, arg) {
					if i+1 < len(args) {
						i++
					} else {
						ambiguous = true
					}
					continue
				}
				if strings.HasPrefix(arg, "-") {
					ambiguous = true
					break
				}
				if strings.HasPrefix(arg, ">") || strings.HasPrefix(arg, "<") {
					if arg == ">" || arg == ">>" || arg == "<" {
						i++
					}
					continue
				}
				if host, ok := hostToken(arg, true); ok {
					set[host] = struct{}{}
					positionalTargets++
					if positionalTargetLimit(name) == positionalTargets {
						break
					}
				}
			}
		}
	}
	return sortedKeys(set), recognized, ambiguous
}

func splitAttachedOption(arg string) (string, string, bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", "", false
	}
	if option, value, ok := strings.Cut(arg, "="); ok {
		return option, value, value != ""
	}
	for _, option := range []string{"-u", "-o", "-H", "-d", "-X", "-w", "-x", "-A", "-p"} {
		if strings.HasPrefix(arg, option) && len(arg) > len(option) {
			return option, arg[len(option):], true
		}
	}
	return "", "", false
}

func positionalTargetLimit(command string) int {
	switch command {
	case "ssh", "sftp", "ftp", "nc", "netcat", "ping", "traceroute":
		return 1
	default:
		return 0
	}
}

func scpRemoteHost(value string) (string, bool) {
	value = strings.Trim(strings.TrimSpace(value), "'\"")
	colon := strings.Index(value, ":")
	if colon <= 0 {
		return "", false
	}
	left := value[:colon]
	if strings.ContainsAny(left, "/\\") {
		return "", false
	}
	if at := strings.LastIndex(left, "@"); at >= 0 {
		left = left[at+1:]
	}
	return hostToken(left, true)
}

func hostToken(value string, allowSingleLabel bool) (string, bool) {
	value = strings.Trim(strings.TrimSpace(value), "'\"(){}<>,")
	if value == "" || strings.ContainsAny(value, "/\\") && !strings.Contains(value, "://") {
		return "", false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return value, true
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		value = value[at+1:]
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if net.ParseIP(value) != nil || strings.EqualFold(value, "localhost") || fullDomainPattern.MatchString(value) {
		return value, true
	}
	if allowSingleLabel && fullLabelPattern.MatchString(value) {
		return value, true
	}
	return "", false
}

func optionIsTarget(command, option string) bool {
	option = strings.ToLower(option)
	switch command {
	case "curl", "wget":
		return option == "--url" || option == "--target" || option == "--host" || option == "--hostname"
	case "ffuf", "feroxbuster", "dirsearch", "nuclei", "sqlmap", "zap":
		return option == "-u" || option == "--url" || option == "--target" || option == "--host" || option == "--hostname"
	default:
		return option == "--target" || option == "--host" || option == "--hostname"
	}
}

func optionTakesValue(command, option string) bool {
	switch command {
	case "curl":
		switch option {
		case "-o", "--output", "-H", "--header", "-d", "--data", "--data-raw", "--data-binary", "-X", "--request", "-w", "--write-out", "-x", "--proxy", "-A", "--user-agent", "-u", "--user":
			return true
		}
	case "wget":
		switch option {
		case "-O", "--output-document", "-U", "--user-agent", "-e", "--execute", "--header", "--post-data", "--proxy":
			return true
		}
	case "nmap", "masscan":
		switch option {
		case "-p", "--ports", "--top-ports", "-oN", "-oX", "-oG", "-oA", "--script", "--script-args", "--exclude", "--excludefile":
			return true
		}
	case "ffuf", "feroxbuster", "dirsearch":
		switch option {
		case "-w", "--wordlist", "-o", "--output", "-H", "--header", "-X", "--request", "-d", "--data", "-x", "--proxy":
			return true
		}
	case "nuclei", "sqlmap", "zap":
		switch option {
		case "-o", "--output", "-H", "--header", "-X", "--request", "-d", "--data", "-r", "--request-file", "-l", "--list":
			return true
		}
	case "ssh", "scp", "sftp":
		switch option {
		case "-p", "-P", "-i", "-J", "-o", "-F", "-l":
			return true
		}
	}
	return false
}

func shellTokens(command string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == quote {
				quote = 0
			} else if r == '\\' && quote == '"' && i+1 < len(runes) {
				i++
				current.WriteRune(runes[i])
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\r':
			flush()
		case '\n', ';', '|', '&':
			flush()
			tokens = append(tokens, string(r))
			if (r == '|' || r == '&') && i+1 < len(runes) && runes[i+1] == r {
				tokens[len(tokens)-1] += string(r)
				i++
			}
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}

func splitShellSegments(tokens []string) [][]string {
	segments := [][]string{{}}
	for _, token := range tokens {
		if token == ";" || token == "|" || token == "||" || token == "&" || token == "&&" || token == "\n" {
			segments = append(segments, []string{})
			continue
		}
		segments[len(segments)-1] = append(segments[len(segments)-1], token)
	}
	return segments
}

func shellBase(value string) string {
	if at := strings.LastIndex(value, "/"); at >= 0 {
		value = value[at+1:]
	}
	return strings.ToLower(value)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.ToLower(strings.Trim(strings.TrimSuffix(value, "."), "[]"))
}

func matchScope(host string, scope []ScopeEntry) int64 {
	ip := net.ParseIP(host)
	for _, row := range scope {
		switch row.Kind {
		case "root_domain":
			domain := strings.ToLower(strings.TrimSuffix(row.Domain, "."))
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return row.ID
			}
		case "subdomain":
			if host == strings.ToLower(strings.TrimSuffix(row.Domain, ".")) {
				return row.ID
			}
		case "ip", "cidr":
			if ip == nil {
				continue
			}
			_, network, err := net.ParseCIDR(row.Net)
			if err == nil && network.Contains(ip) {
				return row.ID
			}
		}
	}
	return 0
}
