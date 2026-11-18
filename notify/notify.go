// Package notify implements the IM / email push channel adapter layer for vulnerability findings.
//
// Layering: this package is a **leaf package** depending only on the standard library. It knows nothing of the database or the server. Channel config
// is passed in as map[string]any (corresponding to the notification_channels.config JSONB column), and
// content to push is passed in as Message. The benefit of this split: the genuinely error-prone parts — signature computation, UTF-8 truncation, filter matching —
// can be unit tested without PostgreSQL, and the host only needs to orchestrate on the server side.
//
// Concurrency contract: Channel implementations must be **stateless**. The same Channel instance is reused concurrently by multiple channel configs
// (even multiple bot instances of the same channel); all credentials are passed in via the cfg parameter,
// and things like the webhook URL must not be cached in the implementation's own fields.
package notify

// Channel type identifiers. The values are also the valid set for notification_channels.kind, validated by a whitelist
// on the server side (same as findings.status: no DB CHECK, to make adding channels easier later).
const (
	KindDingTalk = "dingtalk" // DingTalk custom bot
	KindFeishu   = "feishu"   // Feishu (incl. Lark) custom bot
	KindWeCom    = "wecom"    // WeCom (WeChat Work) group bot
	KindWebhook  = "webhook"  // Generic Webhook: custom method/headers/JSON template
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email
)

// Event types, corresponding to notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback value for an empty kind in config.
const InitKind = KindDingTalk

// severityRank maps finding severities to comparable ordinals. Unknown severities return 0, so any
// min_severity setting keeps unknown severities out — when in doubt, don't push, to avoid spamming false alarms.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns the severity's ordinal; unknown severities return 0.
func SeverityRank(severity string) int { return severityRank[severity] }

// SeverityLabel returns the severity name with an emoji, used for message titles and card colors.
// Unknown severities are echoed as-is, not invented.
func SeverityLabel(severity string) string {
	switch severity {
	case "critical":
		return "🔴 Critical"
	case "high":
		return "🟠 High"
	case "medium":
		return "🟡 Medium"
	case "low":
		return "🔵 Low"
	default:
		return severity
	}
}

// StatusLabel translates a triage status into a display label, used in status-change messages.
func StatusLabel(status string) string {
	switch status {
	case "pending":
		return "Pending"
	case "in_progress":
		return "In progress"
	case "confirmed":
		return "Confirmed"
	case "resolved":
		return "Resolved"
	case "fixed":
		return "Fixed"
	case "false_positive":
		return "False positive"
	case "ignored":
		return "Ignored"
	case "duplicate":
		return "Duplicate"
	case "risk_accepted":
		return "Risk accepted"
	default:
		return status
	}
}

// AtLeast reports whether severity meets the min threshold. An empty min means no threshold; everything passes.
// Note an unknown severity has ordinal 0 and is rejected by any non-empty min (see the severityRank comment).
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}
