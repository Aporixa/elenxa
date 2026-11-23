package traffic

import (
	"encoding/base64"
	"testing"

	"github.com/Aporixa/elenxa/db"
)

func TestProxyTaskID(t *testing.T) {
	h := "Basic " + base64.StdEncoding.EncodeToString([]byte("task-42:"))
	if id, ok := proxyTaskID(h); !ok || id != 42 {
		t.Fatalf("id=%d ok=%v", id, ok)
	}
	if _, ok := proxyTaskID("Basic !!!"); ok {
		t.Fatal("invalid credential accepted")
	}
}

func TestReserveRequestUsesTaskWideCap(t *testing.T) {
	tr := &Traffic{policies: map[int64]db.EngagementPolicy{}, policyState: map[string]*policyRuntime{}}
	p := db.EngagementPolicy{HTTPMaxRequests: 1}
	if _, ok := tr.reserveRequest("7|*", "7|one", p); !ok {
		t.Fatal("first request blocked")
	}
	tr.releasePolicyKeyForTest("7|one")
	if _, ok := tr.reserveRequest("7|*", "7|two", p); ok {
		t.Fatal("task-wide cap did not apply across hosts")
	}
}

func (t *Traffic) releasePolicyKeyForTest(key string) {
	t.policyMu.Lock()
	defer t.policyMu.Unlock()
	if state := t.policyState[key]; state != nil {
		state.inFlight = 0
	}
}
