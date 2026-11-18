package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Autumn-27/norma/permission"
)

// Deferred tools: a mechanism to keep a tool callable while withholding its schema
// from the model's tool list (saving context tokens). Only the tool NAME surfaces
// (via RenderDeferredToolsBlock, placed by the host in the cached system prompt).
// The model discovers a deferred tool's schema with SearchExtraTools, then invokes
// it with ExecuteExtraTool. Both access tools stay in the model's list (never
// deferred). See docs/mcp-deferred-tools-方案设计.md.

// Fixed names of the two always-loaded access tools.
const (
	SearchExtraToolsName = "SearchExtraTools"
	ExecuteExtraToolName = "ExecuteExtraTool"
)

// UnlockSet is a session-scoped, concurrency-safe set of deferred-tool names that
// are currently callable via ExecuteExtraTool. Globally-available deferred tools
// are seeded at construction; skill-gated ones are added at runtime when their
// skill loads. Withholding the schema (deferred) is separate from gating the call
// (unlock): a name may be listed yet locked until its skill is invoked.
type UnlockSet struct {
	mu    sync.RWMutex
	names map[string]bool
}

// NewUnlockSet builds an unlock set seeded with the given tool names.
func NewUnlockSet(initial ...string) *UnlockSet {
	s := &UnlockSet{names: make(map[string]bool, len(initial))}
	for _, n := range initial {
		s.names[n] = true
	}
	return s
}

// Add unlocks the given tool names (idempotent).
func (s *UnlockSet) Add(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		s.names[n] = true
	}
}

// Has reports whether name is currently unlocked (callable).
func (s *UnlockSet) Has(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.names[name]
}

// List returns the unlocked names, sorted.
func (s *UnlockSet) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.names))
	for n := range s.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// RenderDeferredToolsBlock renders the <available-deferred-tools> block: the given
// tool names (one per line, sorted) plus the fixed usage instruction. Returns "" if
// names is empty. Hosts place this in the CACHED system-prompt prefix (a SystemPrompt
// segment before DynamicBoundary) — see design doc §2.1 / C1.
func RenderDeferredToolsBlock(names []string) string {
	if len(names) == 0 {
		return ""
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("<available-deferred-tools>\n")
	for _, n := range sorted {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	b.WriteString("</available-deferred-tools>\n")
	b.WriteString("IMPORTANT: The tools listed above are deferred — they are NOT in your tool list. ")
	b.WriteString("To use one you MUST first discover it via " + SearchExtraToolsName +
		" (returns its schema), then invoke it via " + ExecuteExtraToolName + ".\n")
	b.WriteString("Tool priority: use core tools for core tasks (e.g. bash/curl for HTTP); " +
		"only use " + ExecuteExtraToolName + " for a deferred tool when the task truly needs it.")
	return b.String()
}

// NewSearchExtraTools builds the always-loaded discovery tool. deferred is the set
// of tool names whose schemas are withheld; the tool searches those names in reg
// and returns each match's name + description + params schema.
func NewSearchExtraTools(reg *Registry, deferred []string) CoreTool {
	deferredSet := make(map[string]bool, len(deferred))
	for _, n := range deferred {
		deferredSet[n] = true
	}
	ordered := append([]string(nil), deferred...)
	sort.Strings(ordered)
	return Build(Spec{
		Name: SearchExtraToolsName,
		Description: "Discover deferred tools (not in your tool list) and get their schemas, then invoke via " +
			ExecuteExtraToolName + ". Query forms: \"select:Name\" exact, \"select:A,B\" multiple, or free keywords to match name/description.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "\"select:<name>\" exact (comma-separated for multiple), or keywords.",
				},
			},
			"required": []any{"query"},
		},
		ReadOnly: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(_ context.Context, in json.RawMessage, _ *ToolContext) (Result, error) {
			var args struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(in, &args)
			q := strings.TrimSpace(args.Query)
			if q == "" {
				return Errorf("Error: query is required"), nil
			}
			var matches []CoreTool
			seen := map[string]bool{}
			addByFullName := func(name string) {
				if seen[name] || !deferredSet[name] {
					return
				}
				if t, ok := reg.Get(name); ok {
					seen[name] = true
					matches = append(matches, t)
				}
			}
			// addByQuery resolves one query token: exact full-name first, then
			// case-insensitive suffix match against each deferred tool name so that
			// short MCP tool names like "list_projects" also find
			// "mcp__scopesentry__list_projects".
			addByQuery := func(token string) {
				token = strings.TrimSpace(token)
				if token == "" {
					return
				}
				// 1. exact match
				addByFullName(token)
				if seen[token] {
					return
				}
				// 2. suffix / substring match (case-insensitive) for short names
				lower := strings.ToLower(token)
				for _, name := range ordered {
					lname := strings.ToLower(name)
					// match if full name equals, ends with __token, or contains token
					if lname == lower ||
						strings.HasSuffix(lname, "__"+lower) ||
						strings.Contains(lname, lower) {
						addByFullName(name)
					}
				}
			}
			if strings.HasPrefix(q, "select:") {
				for _, part := range strings.Split(strings.TrimPrefix(q, "select:"), ",") {
					addByQuery(part)
				}
			} else {
				// Multi-token keyword search: every space-separated token must appear
				// in the normalised haystack (__ replaced by space for MCP tool names).
				tokens := strings.Fields(strings.ToLower(q))
				for _, name := range ordered {
					t, ok := reg.Get(name)
					if !ok {
						continue
					}
					// normalise __ → space so "scopesentry list_projects" finds
					// "mcp__scopesentry__list_projects".
					haystack := strings.ReplaceAll(
						strings.ToLower(t.Name()+" "+t.Description()), "__", " ")
					allMatch := true
					for _, tok := range tokens {
						if !strings.Contains(haystack, tok) {
							allMatch = false
							break
						}
					}
					if allMatch {
						addByFullName(name)
					}
				}
			}
			if len(matches) == 0 {
				return Text("No matching deferred tools for query: " + q), nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Found %d deferred tool(s). Invoke with %s({\"tool_name\":\"<name>\",\"params\":{...}}):\n",
				len(matches), ExecuteExtraToolName)
			for _, t := range matches {
				schemaJSON, _ := json.Marshal(t.InputSchema())
				fmt.Fprintf(&b, "\n## %s\n%s\nparams schema: %s\n", t.Name(), t.Description(), string(schemaJSON))
			}
			return Text(b.String()), nil
		},
	})
}

// DelegatingTool is implemented by tools that merely dispatch to another tool
// in the registry (ExecuteExtraTool). The harness uses Unwrap to run the
// permission pipeline against the INNER tool — its name (so allow/deny rules
// and the host's approval callback see e.g. "mcp__srv__tool", not
// "ExecuteExtraTool"), its input, its read-only flag and its own
// CheckPermissions verdict — exactly as if the model had called it directly.
// ok=false means the input does not name a resolvable inner tool (the call will
// fail in Call without side effects).
type DelegatingTool interface {
	CoreTool
	Unwrap(input json.RawMessage) (inner CoreTool, innerInput json.RawMessage, ok bool)
	// Rewrap rebuilds this tool's input around a (possibly permission-rewritten)
	// inner input.
	Rewrap(input json.RawMessage, innerInput json.RawMessage) json.RawMessage
}

// executeExtraTool wraps the built ExecuteExtraTool with permission delegation.
type executeExtraTool struct {
	CoreTool
	reg *Registry
}

type executeExtraArgs struct {
	ToolName string          `json:"tool_name"`
	Params   json.RawMessage `json:"params"`
}

// Unwrap resolves the inner tool and its params (see DelegatingTool).
func (e *executeExtraTool) Unwrap(input json.RawMessage) (CoreTool, json.RawMessage, bool) {
	var args executeExtraArgs
	if err := json.Unmarshal(input, &args); err != nil || args.ToolName == "" {
		return nil, nil, false
	}
	if isAccessTool(args.ToolName) {
		return nil, nil, false
	}
	t, ok := e.reg.Get(args.ToolName)
	if !ok {
		return nil, nil, false
	}
	params := args.Params
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	return t, params, true
}

// Rewrap rebuilds the ExecuteExtraTool input around a new inner params object.
func (e *executeExtraTool) Rewrap(input json.RawMessage, innerInput json.RawMessage) json.RawMessage {
	var args executeExtraArgs
	_ = json.Unmarshal(input, &args)
	args.Params = innerInput
	out, err := json.Marshal(args)
	if err != nil {
		return input
	}
	return out
}

// IsReadOnly reports the inner tool's read-only flag (false when unresolvable),
// so plan mode and the read-only fast path judge the real operation.
func (e *executeExtraTool) IsReadOnly(input json.RawMessage) bool {
	if t, params, ok := e.Unwrap(input); ok {
		return t.IsReadOnly(params)
	}
	return false
}

// CheckPermissions delegates to the inner tool (defense in depth for callers
// that do not unwrap via DelegatingTool): an inner name matching a deny rule is
// denied, otherwise the inner tool's own verdict is returned. An unresolvable
// call is allowed through to Call, which rejects it without side effects.
func (e *executeExtraTool) CheckPermissions(ctx context.Context, input json.RawMessage, pc permission.Context) permission.Decision {
	t, params, ok := e.Unwrap(input)
	if !ok {
		return permission.Allowed()
	}
	if permission.MatchRule(pc.Disallowed, t.Name()) {
		return permission.Denied("denied: tool '" + t.Name() + "' is disallowed by policy")
	}
	d := t.CheckPermissions(ctx, params, pc)
	if len(d.UpdatedInput) > 0 {
		d.UpdatedInput = e.Rewrap(input, d.UpdatedInput)
	}
	return d
}

func isAccessTool(name string) bool {
	return name == ExecuteExtraToolName || name == SearchExtraToolsName
}

// NewExecuteExtraTool builds the always-loaded invoker for deferred tools. It runs
// the named tool from reg, but only if it is currently in the unlock set. Schema is
// validated before the call. unlock may be nil (then every registry tool is
// callable — no gating).
//
// Permission: the returned tool implements DelegatingTool, and the harness
// evaluates the permission pipeline against the inner tool (name, input,
// read-only flag, CheckPermissions) — deferred/MCP calls get exactly the same
// allow/deny/ask treatment as a direct call. (Upstream self-allowed every call.)
func NewExecuteExtraTool(reg *Registry, unlock *UnlockSet) CoreTool {
	built := Build(Spec{
		Name: ExecuteExtraToolName,
		Description: "Invoke a deferred tool discovered via " + SearchExtraToolsName +
			". tool_name is the deferred tool's exact name; params must match its schema.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool_name": map[string]any{
					"type":        "string",
					"description": "Exact name of the deferred tool (from " + SearchExtraToolsName + " / <available-deferred-tools>).",
				},
				"params": map[string]any{
					"type":        "object",
					"description": "Parameters for the tool, matching its schema.",
				},
			},
			"required": []any{"tool_name"},
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *ToolContext) (Result, error) {
			var args executeExtraArgs
			if err := json.Unmarshal(in, &args); err != nil {
				return Errorf("Error: " + err.Error()), nil
			}
			if args.ToolName == "" {
				return Errorf("Error: tool_name is required"), nil
			}
			if isAccessTool(args.ToolName) {
				return Errorf("Error: " + args.ToolName + " cannot be invoked through " + ExecuteExtraToolName), nil
			}
			t, ok := reg.Get(args.ToolName)
			if !ok {
				return Errorf("Error: unknown tool " + args.ToolName), nil
			}
			if unlock != nil && !unlock.Has(args.ToolName) {
				return Errorf("Error: tool " + args.ToolName +
					" is not unlocked in this session — load the skill that provides it first."), nil
			}
			params := args.Params
			if len(params) == 0 {
				params = json.RawMessage("{}")
			}
			if err := ValidateInput(t.InputSchema(), params); err != nil {
				return Errorf("Error: invalid params for " + args.ToolName + ": " + err.Error()), nil
			}
			return t.Call(ctx, params, tc)
		},
	})
	return &executeExtraTool{CoreTool: built, reg: reg}
}
