package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Aporixa/elenxa/db"
	actool "github.com/Autumn-27/norma/tool"
)

func TestEngagementToolBlocksUnboundedScannerButNotOrdinaryShell(t *testing.T) {
	events := 0
	wrapped := &engagementTool{CoreTool: actool.NewBash(), policy: db.EngagementPolicyForProfile("quiet"), onEvent: func(string, string) { events++ }}
	result, err := wrapped.Call(context.Background(), json.RawMessage(`{"command":"nmap -sV 127.0.0.1"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Flatten(), "blocked by engagement policy") || events != 1 {
		t.Fatalf("result=%q events=%d", result.Flatten(), events)
	}
}

func TestEngagementToolAllowsBoundedScanner(t *testing.T) {
	p := db.EngagementPolicyForProfile("quiet")
	if got := scannerLimitGuidance("nmap", "nmap --max-rate 5 --max-parallelism 2 127.0.0.1", p); got != "" {
		t.Fatalf("unexpected block: %s", got)
	}
	if got := scannerLimitGuidance("nmap", "nmap --max-rate 500 --max-parallelism 20 127.0.0.1", p); !strings.Contains(got, "got 500") {
		t.Fatalf("excessive value was not blocked: %s", got)
	}
}
