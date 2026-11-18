package agent

import (
	"strings"

	"github.com/Autumn-27/norma/harness"
)

// Wrap-up prompt (wrap-up / settlement prompt): when an agent is terminated because of
// [steps exhausted (MaxTurns)] or [timeout (run_seconds/MaxDuration)], the SDK's settlement
// phase injects this prompt so the agent first persists what it identified but has not
// written back, then outputs a one-sentence summary, avoiding an unfinished ending.
//
// Each agent's wrap-up prompt can be overridden in the admin UI as needed (stored in
// agents.wrapup_prompt); left empty, the built-in default here is used. Only the [prompt
// body] is editable; which tools are disabled and the wrap-up's own turn budget are fixed
// code policy.

// WrapupOverride, if set, returns the stored wrap-up prompt for an agent key and
// whether a non-empty one exists. Wired by the server to the agents table (like
// PromptOverride for system prompts). nil / empty → the built-in default is used.
var WrapupOverride func(agentKey string) (string, bool)

// WrapupMaxTurnsOverride, if set, returns the admin-configured turn budget for the
// wrap-up phase of an agent and whether a positive one exists. Wired to the agents
// table. nil / ≤0 → the built-in per-agent default (wrapupTurnDefaults) is used.
var WrapupMaxTurnsOverride func(agentKey string) (int, bool)

// Built-in default wrap-up prompts, indexed by agent key. worker reuses the historically
// hard-coded settleWrapUpPrompt (defined in worker.go), planner/mainagent each have their
// own; misses (custom agents) use the generic fallback.
var wrapupDefaults = map[string]string{
	"worker":    settleWrapUpPrompt,
	"planner":   plannerWrapUpDefault,
	"mainagent": mainAgentWrapUpDefault,
}

// wrapupTurnDefaults: built-in default turn budget for each agent's wrap-up phase [itself]
// (can be overridden by an admin value >0). All get 10 turns so the wrap-up phase has
// enough steps to persist results. Misses use genericWrapupTurns.
var wrapupTurnDefaults = map[string]int{
	"worker":    10,
	"planner":   10,
	"mainagent": 10,
}

const genericWrapupTurns = 10

const plannerWrapUpDefault = "Your planning steps for this round are about to run out - note that only [this round] is ending; the system will wake you again to continue planning as the situation changes, the task is not terminating, and you do not need to wrap up the whole plan here. Land the conclusions you have already worked out this round so it is not wasted, but also [do not force intents just to wrap up] (0 intents this round is a perfectly normal result): (1) if you have identified exploration directions that [should be dispatched now], submit them in one batched add_intent (don't hold back ones you have worked out); (2) for goals already proven achieved by some finding/fact, call prove_goal to mark them met (don't miss any); (3) if you identified a serial exploit chain that needs to be done step by step, record it with TodoWrite so you can continue dispatching at the next wake-up. When done, end this round directly; no summary text is needed."

const mainAgentWrapUpDefault = "Your steps are about to run out and this interaction is about to end. Do not start any new exploration/operation. **In a single standalone sentence of plain text**, summarize for the user the current progress, key conclusions, and the suggested next step."

const genericWrapUpDefault = "You are about to be terminated because your budget is exhausted. First write back results that are complete but not yet persisted, then **in a single standalone sentence of plain text** summarize what you did and the key conclusions you reached (this sentence will be shown as the result of this run)."

// WrapupDefault returns the built-in default wrap-up prompt for an agent key —
// used by the admin UI as the "restore default" value and empty-field placeholder.
func WrapupDefault(agentKey string) string {
	if d, ok := wrapupDefaults[agentKey]; ok {
		return d
	}
	return genericWrapUpDefault
}

// WrapupTurnsDefault returns the built-in wrap-up turn budget for an agent key —
// used by the admin UI as the "0 = default N" hint.
func WrapupTurnsDefault(agentKey string) int {
	if n, ok := wrapupTurnDefaults[agentKey]; ok {
		return n
	}
	return genericWrapupTurns
}

// resolveWrapup returns the effective wrap-up prompt: the DB override (if set and
// non-empty) over the built-in default.
func resolveWrapup(agentKey string) string {
	if WrapupOverride != nil {
		if t, ok := WrapupOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return WrapupDefault(agentKey)
}

// resolveWrapupTurns returns the effective wrap-up turn budget: a positive DB
// override over the built-in per-agent default.
func resolveWrapupTurns(agentKey string) int {
	if WrapupMaxTurnsOverride != nil {
		if v, ok := WrapupMaxTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return WrapupTurnsDefault(agentKey)
}

// wrapupSettlement builds the settlement config for an agent's run. Prompt and the
// turn budget are admin-editable per agent; disabled tools are code-owned policy so
// a user can't edit away the "stop probing" guardrail. Resolved fresh each run
// (reads DB live), so edits apply on the next run without a restart.
func wrapupSettlement(agentKey string, disabledTools []string) *harness.Settlement {
	return &harness.Settlement{
		Prompt:        resolveWrapup(agentKey),
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
}

// ---------- Task-level timeout wrap-up prompts (see docs/任务级超时与收尾设计.md) ----------
//
// These are [separate] from the per-run wrap-up prompts: per-run means "your budget for this
// run is used up"; task timeout means "the whole task's time is up and it is about to end".
// The semantics are often opposite (especially for planner: per-run says "don't stop, keep
// planning", task timeout says "time's up, stop planning and make the final judgment").
// Configured only for worker/planner.

// WrapupTaskTimeoutOverride / …TurnsOverride: DB overrides for the task-timeout wrap-up
// prompt and turn count (wired to agents.task_timeout_wrapup_prompt / _max_turns, worker/planner only).
var (
	WrapupTaskTimeoutOverride      func(agentKey string) (string, bool)
	WrapupTaskTimeoutTurnsOverride func(agentKey string) (int, bool)
)

var taskTimeoutWrapupDefaults = map[string]string{
	"worker":  workerTaskTimeoutDefault,
	"planner": plannerTaskTimeoutDefault,
}

const workerTaskTimeoutDefault = "**The whole task has reached its timeout limit and is about to end** (not your budget for this run - the entire exploration's time is up). This is the last chance: (1) persist [everything] you have identified but not yet written back - new assets via insert_assets, exploration conclusions/facts via record_fact, confirmed vulnerabilities via report_finding; (2) do not start any new commands/probes; (3) **finally, in a single standalone sentence of plain text**, summarize your key conclusions on this intent."

const plannerTaskTimeoutDefault = "**The whole task has reached its timeout limit and is about to end** (not this round - the entire task is terminating). Based on [all] current facts and findings, make the final goal judgment: for goals proven achieved by evidence, call prove_goal to mark them met (don't miss any). **Do not generate any new intents** (intents dispatched now will not be executed anymore). Once judged, wrap up; no summary text is needed."

// TaskTimeoutWrapupDefault returns an agent's built-in default task-timeout wrap-up prompt (for admin placeholder/restore default).
func TaskTimeoutWrapupDefault(agentKey string) string {
	return taskTimeoutWrapupDefaults[agentKey] // not configured (mainagent/chat) returns empty string
}

// resolveTaskTimeoutWrapup: DB override (non-empty) > built-in default. An empty string means
// the agent has no task-timeout prompt (not worker/planner); the caller should then fall back to the per-run prompt.
func resolveTaskTimeoutWrapup(agentKey string) string {
	if WrapupTaskTimeoutOverride != nil {
		if t, ok := WrapupTaskTimeoutOverride(agentKey); ok && strings.TrimSpace(t) != "" {
			return t
		}
	}
	return TaskTimeoutWrapupDefault(agentKey)
}

func resolveTaskTimeoutTurns(agentKey string) int {
	if WrapupTaskTimeoutTurnsOverride != nil {
		if v, ok := WrapupTaskTimeoutTurnsOverride(agentKey); ok && v > 0 {
			return v
		}
	}
	return resolveWrapupTurns(agentKey) // defaults to the per-run turn count
}

// wrapupSettlementForTask builds settlement for a worker/planner run that is aware
// of the task deadline. See §5 of the design doc:
//   - clamped=true  → this run is clamped by the task deadline: wrap-up due to Timeout = task time is up → task-timeout prompt;
//     wrap-up due to MaxTurns = steps ran out within the clamped window with minutes left on the task → fall back to the per-run prompt.
//   - clamped=false → the task has plenty of time: both reasons use the per-run prompt (i.e. degrades to wrapupSettlement).
//
// Handed to the harness's PromptByReason, which picks by the [actual] reason at wrap-up time, avoiding a build-time mismatch.
func wrapupSettlementForTask(agentKey string, disabledTools []string, clamped bool) *harness.Settlement {
	perRun := resolveWrapup(agentKey)
	st := &harness.Settlement{
		Prompt:        perRun, // fallback (also the value for both reasons when not clamped)
		DisabledTools: disabledTools,
		MaxTurns:      resolveWrapupTurns(agentKey),
	}
	if clamped {
		if tt := resolveTaskTimeoutWrapup(agentKey); tt != "" {
			st.PromptByReason = map[harness.TerminalReason]string{
				harness.ReasonTimeout:  tt,     // task time is up
				harness.ReasonMaxTurns: perRun, // steps ran out first, task still has time
			}
			st.MaxTurns = resolveTaskTimeoutTurns(agentKey)
		}
	}
	return st
}
