package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"sync"

	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

// DeferredInfo carries the deferred-tools wiring an agent needs to build its
// Options: the MCP tool names whose schemas are withheld, the subset listed in the
// global system-prompt block (non-skill-gated), and the shared session unlock set.
// UnlockSkill unlocks a named skill's MCPs — hosts call it to rebuild the unlock set
// from history on a resumed session (design doc C2).
type DeferredInfo struct {
	FindingGuidance string            // derived from the final permitted tools, including DB overrides
	Deferred        []string          // all MCP tool names (schema withheld)
	GlobalNames     []string          // MCP names to list in the system-prompt block
	Unlock          *actool.UnlockSet // shared call-gate; nil when no MCP tools
	UnlockSkill     func(skillName string)
}

// ToolAugment, if set, returns the EXTRA tools an agent should see beyond its
// built-in base set — the agent's visible skills (packed into one Skill meta-tool)
// and visible MCP servers (expanded to mcp__server__tool). It also returns the
// DeferredInfo describing how those MCP tools are deferred/gated. The server wires
// it to the PG agent_visibility table. cleanup releases any spawned MCP clients.
//
// When nil, agents run with only their built-in tools — behavior is unchanged
// until a user assigns a skill/MCP to the agent in the UI.
var ToolAugment func(ctx context.Context, agentKey string) (extra []actool.CoreTool, def DeferredInfo, cleanup func())

// AugmentTools returns base plus the agent's visible skill/MCP tools, the
// DeferredInfo, and a cleanup func the caller must defer (closes MCP clients).
// Built-in base tools are kept as-is — never filtered (built-in tools stay in the code layer, no visibility filtering).
func AugmentTools(ctx context.Context, agentKey string, base []actool.CoreTool) ([]actool.CoreTool, DeferredInfo, func()) {
	var (
		def     DeferredInfo
		cleanup = func() {}
		out     = base
	)
	if ToolAugment != nil {
		var extra []actool.CoreTool
		var cl func()
		extra, def, cl = ToolAugment(ctx, agentKey)
		if cl != nil {
			cleanup = cl
		}
		if len(extra) > 0 {
			out = append(append([]actool.CoreTool{}, base...), extra...)
		}
	}
	// DB tools table has the final say on the built-in tools: drop the ones this
	// agent isn't bound to (or that are disabled) and swap in overridden
	// descriptions/schemas + default injection. MCP/skill/host tools have no row
	// and pass through untouched, so deferred/unlock wiring stays consistent.
	if ToolResolve != nil {
		out = ToolResolve(ctx, agentKey, out)
	}
	out, def.FindingGuidance = findingWorkflowTools(agentKey, out)
	duplicates := &duplicateCallGuard{}
	for i, t := range out {
		out[i] = guardDuplicateCalls(guardPanic(t), duplicates)
	}
	return out, def, cleanup
}

// duplicateCallGuard is scoped to one assembled agent run. A different call
// resets the streak, so intentional verification after gathering new evidence
// is unaffected; only immediate, identical loops are intercepted.
type duplicateCallGuard struct {
	mu          sync.Mutex
	last        [sha256.Size]byte
	hasLast     bool
	consecutive int
}

func (g *duplicateCallGuard) observe(name string, input json.RawMessage) int {
	normalized := input
	var value any
	if json.Unmarshal(input, &value) == nil {
		if raw, err := json.Marshal(value); err == nil {
			normalized = raw
		}
	}
	fingerprint := sha256.Sum256(append(append([]byte(name), 0), normalized...))
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.hasLast && fingerprint == g.last {
		g.consecutive++
	} else {
		g.last = fingerprint
		g.hasLast = true
		g.consecutive = 1
	}
	return g.consecutive
}

func guardDuplicateCalls(t actool.CoreTool, guard *duplicateCallGuard) actool.CoreTool {
	return &duplicateGuardedTool{CoreTool: t, guard: guard}
}

type duplicateGuardedTool struct {
	actool.CoreTool
	guard *duplicateCallGuard
}

func (d *duplicateGuardedTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	if d.guard == nil {
		return d.CoreTool.Call(ctx, in, tc)
	}
	consecutive := d.guard.observe(d.Name(), in)
	if consecutive >= 3 {
		return actool.Errorf("duplicate-call prevention: blocked a third consecutive identical " + d.Name() + " call; the prior retry produced no changed plan. Check the control path and existing evidence, then change the tool or arguments before retrying"), nil
	}
	result, err := d.CoreTool.Call(ctx, in, tc)
	if consecutive == 2 {
		result.Content = append(result.Content, llm.TextBlock("[Duplicate-call warning] This was the second consecutive identical call. Do not issue it again unchanged; check the control path, use the existing result, or change the hypothesis/arguments."))
	}
	return result, err
}

// guardPanic turns a panicking tool handler into an ordinary tool error. The
// harness runs each tool on its own goroutine, so a panic inside a handler can't
// be recovered by the caller that started the run — it takes the whole process
// down, and on restart the agent replays the same call and crashes again. Applied
// last, so it covers every tool the agent can reach: domain, SDK, MCP and skill.
func guardPanic(t actool.CoreTool) actool.CoreTool { return &guardedTool{CoreTool: t} }

type guardedTool struct{ actool.CoreTool }

func (g *guardedTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (res actool.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tools] %s panic: %v\n%s", g.Name(), r, debug.Stack())
			res, err = actool.Errorf(fmt.Sprintf("tool %s internal error: %v (this call failed; try different parameters or use another tool)", g.Name(), r)), nil
		}
	}()
	return g.CoreTool.Call(ctx, in, tc)
}
