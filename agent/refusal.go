package agent

import "errors"

// ErrModelRefused is returned when the model provider declined a run
// (Anthropic stop_reason "refusal"). It is deterministic for the same request,
// so callers must not retry it; the engine pauses the task instead.
var ErrModelRefused = errors.New("model provider declined the request (stop_reason=refusal)")
