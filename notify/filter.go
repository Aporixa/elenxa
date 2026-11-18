package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the contract for the notification_channels.filter JSONB column: a channel instance's filter conditions.
// All fields are optional; omitted means "no filtering" — which is exactly the fallback semantics for malformed config, see ParseFilter.
type Filter struct {
	// MinSeverity is the minimum severity threshold (low/medium/high/critical); empty = no threshold.
	MinSeverity string `json:"min_severity"`
	// An empty TaskIDs / AssetIDs array means unrestricted; non-empty requires the event to intersect it.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// An empty VulnClassInclude accepts everything; non-empty requires vulnclass to match any of the keywords.
	// Matching any VulnClassExclude keyword excludes the event (exclude takes precedence over include).
	// Matching is a case-insensitive substring — safer than regex: a user misconfiguring a regex will not silently break the channel.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange decides whether the channel receives finding status-change events (only meaningful in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses a channel's filter config.
//
// **Never returns an error.** This is a deliberate design choice: malformed filter config always degrades to the zero-value
// Filter (= no filtering = everything matches), because for a vulnerability notification system **pushing one extra is far better than
// silently missing a high-severity one**. Turning a parse failure into "don't push" would give the user a channel that looks configured
// but actually pushes nothing — the worst failure mode.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure f stays the zero value, i.e. no filtering.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid severity threshold (empty string means no threshold).
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks the **value-restricted** fields in filter config, for use when saving a channel.
//
// Why this must be blocked at write time: Match's check for an unknown threshold is `rank >= 0`, which is always true —
// that is, a typo in min_severity ("hgih") makes the filter **silently fail** and become
// "push everything". This is consistent with this package's "better to over-push than miss" trade-off (nothing gets missed),
// but the consequence is the user thinks they are doing severity-tiered pushes while actually flooding the group with every finding,
// with no sign they misconfigured it. This kind of "silent degradation" is exactly what should be blocked at the entry point.
//
// Note Validate is used only on the **write** path. The read path still uses ParseFilter's lenient semantics,
// so bad values already in historical data won't make the channel unreadable altogether.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("invalid minimum severity %q; options: low / medium / high / critical, or leave empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should be delivered to a channel with this filter.
//
// **Never returns an error**, for the same reason as ParseFilter: any internal anomaly is treated as a "match".
// Evaluation order: event type → severity threshold → task/asset scope → vulnclass keywords.
func Match(f Filter, s Snapshot) bool {
	// Status-change events are received only by channels that explicitly enable them. Off by default, because most users
	// expect "push" to mean "new finding discovered", not a running log following every status transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclude first: matching any exclude keyword rules it out, even if it also matches the include list.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// A linear scan is fine for small sets; both sides are on the order of "a few dozen hand-picked items",
	// so building a map costs more than it gains.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any of keywords (case-insensitive).
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
