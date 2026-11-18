package llm

import (
	"encoding/json"
	"testing"
)

func TestBuildAnthropicSystemCacheBoundary(t *testing.T) {
	sys := []string{"a", "b", "c"}

	cases := []struct {
		name       string
		boundary   int
		wantBlocks int
		wantCached []bool // per-block: has cache_control
	}{
		{"no boundary (0) → whole thing cached", 0, 1, []bool{true}},
		{"negative → whole thing cached", -1, 1, []bool{true}},
		{"mid split → cached prefix + dynamic tail", 2, 2, []bool{true, false}},
		{"boundary == len → whole thing cached", 3, 1, []bool{true}},
		{"boundary > len → whole thing cached", 5, 1, []bool{true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blocks := buildAnthropicSystem(CompletionRequest{System: sys, DynamicBoundary: c.boundary})
			if len(blocks) != c.wantBlocks {
				t.Fatalf("blocks=%d want %d", len(blocks), c.wantBlocks)
			}
			for i, wantCached := range c.wantCached {
				gotCached := blocks[i].CacheControl != nil
				if gotCached != wantCached {
					t.Fatalf("block %d cached=%v want %v", i, gotCached, wantCached)
				}
			}
		})
	}
}

func TestBuildAnthropicSystemCacheOff(t *testing.T) {
	SetPromptCache(false)
	defer SetPromptCache(true)
	for _, b := range []int{0, 2, 3} {
		blocks := buildAnthropicSystem(CompletionRequest{System: []string{"a", "b", "c"}, DynamicBoundary: b})
		if len(blocks) != 1 || blocks[0].CacheControl != nil {
			t.Fatalf("boundary %d with cache off: got %+v, want one uncached block", b, blocks)
		}
	}
}

// TestAnthropicBodyPromptCache checks the full request body: with caching on it
// carries top-level automatic caching plus a system breakpoint; with it off there
// is no cache_control anywhere in the JSON.
func TestAnthropicBodyPromptCache(t *testing.T) {
	p := &anthropicProvider{cfg: Config{Model: "m"}}
	req := CompletionRequest{
		System:   []string{"you are a test"},
		Messages: []Message{UserText("hi")},
	}
	body := func() map[string]any {
		raw, err := p.buildBody(req, true)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	on := body()
	if cc, ok := on["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Fatalf("cache on: top-level cache_control = %v, want ephemeral", on["cache_control"])
	}
	sys := on["system"].([]any)
	if _, ok := sys[len(sys)-1].(map[string]any)["cache_control"]; !ok {
		t.Fatalf("cache on: system has no breakpoint: %v", sys)
	}

	SetPromptCache(false)
	defer SetPromptCache(true)
	raw, _ := p.buildBody(req, true)
	if json.Valid(raw) && containsKey(raw, "cache_control") {
		t.Fatalf("cache off: body still contains cache_control: %s", raw)
	}
}

func containsKey(raw []byte, key string) bool {
	var walk func(v any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, vv := range x {
				if k == key || walk(vv) {
					return true
				}
			}
		case []any:
			for _, vv := range x {
				if walk(vv) {
					return true
				}
			}
		}
		return false
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return walk(v)
}
