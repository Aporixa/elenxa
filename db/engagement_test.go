package db

import "testing"

func TestNormalizeEngagementPolicyPresetsAndUserAgent(t *testing.T) {
	p, err := NormalizeEngagementPolicy(EngagementPolicy{Profile: "quiet", UserAgentMode: "fixed", UserAgent: "Elenxa research/1"})
	if err != nil {
		t.Fatal(err)
	}
	if p.HTTPRequestsPerSecond != 2 || p.HTTPMaxConcurrentPerHost != 2 || p.UserAgent != "Elenxa research/1" {
		t.Fatalf("unexpected policy: %#v", p)
	}
	if _, err := NormalizeEngagementPolicy(EngagementPolicy{Profile: "custom", UserAgentMode: "fixed"}); err == nil {
		t.Fatal("empty fixed user agent should fail")
	}
}

func TestNormalizeEngagementPolicyOldClientDefaultsToLab(t *testing.T) {
	p, err := NormalizeEngagementPolicy(EngagementPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Profile != "lab" || p.UserAgentMode != "preserve" {
		t.Fatalf("unexpected default: %#v", p)
	}
}
