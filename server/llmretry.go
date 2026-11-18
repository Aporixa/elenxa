package server

import (
	"time"

	"github.com/Aporixa/elenxa/agent"
	"github.com/Aporixa/elenxa/db"
)

// Server-side resolution of the retry policy; see docs/LLM重试设计.md. Of the five layers:
//   - connect / empty response / same-provider safety window "follow the endpoint": each
//     LLM profile can override the global default (a field left blank on the profile
//     inherits the global value; if the global one is unset too, the built-in default applies);
//   - circuit breaker / intent rerun are process-wide, with a single global copy.
//
// The global policy reads one settings row from the DB each time. All call sites are on
// low-frequency paths (building a provider, finishing a work, saving config), so another
// cache layer isn't worth it. The circuit-breaker params are the exception: they're read
// on every failure, so applyRetryPolicy pushes them to the Registry to hold.

// retryPolicy reads the global policy; a nil DB yields the zero policy (all
// layers on their built-in defaults).
func (s *Server) retryPolicy() db.LLMRetryPolicy {
	if s.m == nil || s.m.pg == nil {
		return db.LLMRetryPolicy{}
	}
	return s.m.pg.LLMRetryPolicy()
}

// resolveRetry layers one profile's override on top of the global policy and
// converts the result into the form agent.Config carries. Rules combine field by
// field, so a profile that only pins an interval still inherits the global count.
func resolveRetry(o db.RetryOverride, pol db.LLMRetryPolicy) agent.RetryConfig {
	connect := o.Connect.Or(pol.Connect)
	empty := o.Empty.Or(pol.Empty)
	stream := o.Stream.Or(pol.Stream)
	return agent.RetryConfig{
		// Counts keep their raw "0=default / negative=off" semantics here: the SDK's
		// MaxRetries / EmptyResponseRetries are exactly isomorphic, so let it resolve them.
		ConnectAttempts: connect.Attempts, ConnectInterval: connect.Interval(),
		EmptyAttempts: empty.Attempts, EmptyInterval: empty.Interval(),
		StreamAttempts: stream.Attempts, StreamInterval: stream.Interval(),
	}
}

// applyProfileRetry fills cfg.Retry for a profile read from the DB.
func (s *Server) applyProfileRetry(cfg *agent.Config, p *db.LLMProfile) {
	if p == nil {
		return
	}
	cfg.Retry = resolveRetry(p.Retry, s.retryPolicy())
}

// Circuit-breaker (round-robin cooldown) defaults, matching llmpool's built-ins — only
// overridden here when the user has configured a value.
// Intent-rerun defaults: see modelErrorRetries / modelErrorRetryBackoff in engine.go.

// applyRetryPolicy pushes the process-wide layers of the policy into the objects
// that consume them on a hot path: the circuit-breaker registry. Called at
// startup and whenever the policy is saved.
func (s *Server) applyRetryPolicy() {
	pol := s.retryPolicy()
	if s.llmHealth != nil {
		s.llmHealth.SetPolicy(pol.Breaker.Attempts, pol.Breaker.Interval())
	}
}

// modelErrorRetryPolicy resolves the intent-level replay knobs (layer ⑤): how
// many times a model_error work is re-run and how long to back off between runs.
func (e *Engine) modelErrorRetryPolicy() (retries int, backoff time.Duration) {
	retries, backoff = modelErrorRetries, modelErrorRetryBackoff
	if e == nil || e.m == nil || e.m.pg == nil {
		return retries, backoff
	}
	rule := e.m.pg.LLMRetryPolicy().Intent
	if rule.Attempts != 0 {
		retries = max(rule.Attempts, 0)
	}
	if d := rule.Interval(); d > 0 {
		backoff = d
	}
	return retries, backoff
}

// emptyTurnNudgeLimit resolves how many empty-turn continuations one work may
// inject (see steerHooks.Stop). It deliberately reuses layer ②'s knob — "empty-response
// retry count": both are ways of handling the same thing. The SDK layer handles "not a
// single content block", by resending the same request verbatim; this one handles "only
// thinking, no text and no tools", by appending an instruction so the model continues
// from its existing thinking (a verbatim resend is pointless for a stall caused by the
// shape of the context). The emptiness criteria differ because the SDK goes by "was any
// event yielded", and thinking deltas are themselves events — but when a user sets "how
// many empty-response retries" they mean "try again if the model produced nothing
// substantive", so sharing one count across both layers matches that mental model.
//
// Reads the global policy rather than a profile override: a run may switch profiles
// mid-way due to failover, and this is a total-volume gate for the whole intent that
// shouldn't change with the endpoint. Semantics are isomorphic to the SDK's emptyRetries():
// 0 = default defaultEmptyTurnNudges; -1 (negative) = disable empty-turn continuation; >0 = use that value.
func (e *Engine) emptyTurnNudgeLimit() int {
	if e == nil || e.m == nil || e.m.pg == nil {
		return defaultEmptyTurnNudges
	}
	switch n := e.m.pg.LLMRetryPolicy().Empty.Attempts; {
	case n == 0:
		return defaultEmptyTurnNudges
	case n < 0:
		return 0
	default:
		return n
	}
}
