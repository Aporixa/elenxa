package llm

import "sync/atomic"

// Prompt caching switch for the Anthropic provider (Aporixa research build).
//
// On (the default): every request caches the system prompt (the whole prompt when
// no DynamicBoundary is set, otherwise the static prefix before the boundary) and
// sets top-level automatic caching, which moves a breakpoint to the end of the
// conversation so each turn re-reads the history written by the previous one.
//
// Off: no cache_control is sent anywhere, including at a DynamicBoundary. This
// reproduces the uncached request shape of Run 01 exactly.
//
// Caching is a billing/latency feature only: a cache hit replays the processing of
// a byte-identical prefix, so the model sees the same input either way. The flag is
// read on every request, so a change takes effect on the next model call.
var promptCacheOff atomic.Bool // zero value = caching on

// SetPromptCache turns Anthropic prompt caching on or off process-wide.
func SetPromptCache(on bool) { promptCacheOff.Store(!on) }

// PromptCacheEnabled reports whether Anthropic prompt caching is on.
func PromptCacheEnabled() bool { return !promptCacheOff.Load() }
