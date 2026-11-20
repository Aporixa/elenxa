package knowledge

import (
	"encoding/json"
	"testing"
)

func TestBuiltinSearchIsBoundedAndVersioned(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalog.Search("TLS timeout network MTU", 2, 1800)
	if err != nil {
		t.Fatal(err)
	}
	if result.CorpusVersion == "" || result.CorpusDigest == "" {
		t.Fatalf("missing corpus identity: %+v", result)
	}
	if len(result.Matches) == 0 || len(result.Matches) > 2 || result.Matches[0].ID != "LAB-NET-001" {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	raw, _ := json.Marshal(result.Matches)
	if len(raw) > 1800 {
		t.Fatalf("encoded matches exceed max_chars: %d", len(raw))
	}
}

func TestBuiltinSearchRejectsEmptyQuery(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Search(" ", 3, 4000); err == nil {
		t.Fatal("empty query accepted")
	}
}
