package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets decides whether messages may be delivered to loopback / link-local addresses.
//
// Denied by default. These ranges are not where IM bots or public mail servers live, and what they can
// reach is sensitive: another service's admin port on the same host, and the cloud metadata endpoint
// (169.254.169.254, which can expose instance credentials). The delivery address is admin-configured, but an admin session borrowed via XSS/CSRF,
// or a second person sharing the same JWT, could read response content back by changing the config
// — doJSON writes the first 200 bytes of a 4xx/5xx response body into last_error, and the delivery history endpoint
// echoes it back, which is a semi-blind read primitive.
//
// But a "local SMTP relay" (postfix on 127.0.0.1:25) is a common setup for self-hosted mail,
// and a blanket ban would block people. So we leave an explicit escape hatch instead of hardcoding an allowance:
// set ELENXA_NOTIFY_ALLOW_LOCAL=1 to allow it.
//
// It is exported as AllowLocalTargetsEnv so tests can explicitly turn it on — test cases in this package and the server package
// heavily use httptest fake receivers on 127.0.0.1, and without enabling it they would all be blocked by the guard.
const AllowLocalTargetsEnv = "ELENXA_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP reports whether the target IP belongs to a range that is "not allowed for delivery by default".
//
// Only loopback, link-local (including cloud metadata 169.254.169.254), unspecified, and multicast are rejected.
// RFC1918 private networks are **not rejected**: self-hosted Mattermost / SMTP relays on internal networks are a very common legitimate use,
// and blocking them too would make the feature unusable in real environments. This trade-off is deliberate —
// the protection must block genuinely sensitive targets without breaking normal deployments along with them.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) must be converted back to IPv4 before checking, otherwise it bypasses the check.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the Control hook of the http.Transport dialer, checking the target address **at connection
// time**.
//
// Why at dial time rather than only validating when saving config: this is the final enforcement point.
// It covers both ways of bypassing config validation — DNS rebinding (resolves to a public IP at validation time,
// to an internal one at actual connection time) and redirects (although we already refuse cross-host redirects, same-host redirects
// could still point the path elsewhere).
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("cannot resolve target address %q", host)
	}
	if isBlockedDialIP(ip) {
		return fmt.Errorf("refusing to deliver to local/link-local address %s (if you really need to deliver to a local service, set %s=1)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport adds only a dial guard on top of the default Transport.
// Clone keeps all of the default tuning (connection pool, HTTP/2, timeouts, proxy, etc.),
// to avoid changing other behavior just to add one check.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is the client shared by all channel deliveries.
//
// It deliberately does **not** reuse the project's global egress proxy (GlobalProxy on the server side): that proxy is for pentest
// target traffic and is often an unstable tunnel, and notification availability shouldn't be held hostage by target-network jitter.
// IM pushes can connect directly. The timeout is 15 seconds — a peer slower than that is effectively already in a failed state.
//
// Cross-host redirects are refused: the delivery addresses in this feature are all "a fixed endpoint" and normally never
// redirect to another host; and these providers' credentials (DingTalk access_token, WeCom key, Telegram
// bot token) **are in the URL**, so following a cross-host redirect would hand the credentials to the redirect target. Same-host
// redirects (e.g. adding a trailing slash) are still allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return fmt.Errorf("refusing cross-host redirect (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit limits how much of the response body is read. A misbehaving peer may return huge content, and we only need
// the status code and a short error description to show in delivery history.
const respBodyLimit = 8 << 10

// doJSON sends one request and returns the response body (size-limited).
//
// When payload is nil an empty body is sent (for GET or platforms that don't require a body).
// Key/values in headers are attached as-is, for the generic Webhook's custom headers.
//
// Error classification is this function's core responsibility: network-layer failures and 5xx/408/429 are "retryable",
// other 4xx are "permanent failures" — retrying a 403 just floods the log with the same error 3 times.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// A serialization failure is a local bug (wrong config field type); retrying won't help.
			return nil, Permanent(fmt.Errorf("failed to build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// Invalid URL — most likely the user entered the address wrong, a permanent failure.
		// err must not be passed through here either: url.Parse's error text contains the full address.
		return nil, Permanent(fmt.Errorf("invalid request address: %s", redactRequestTarget(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refused, DNS failure, timeout — mostly transient faults, left to backoff retry.
		//
		// The error text must be redacted before being passed on. Reason: http.Client.Do returns a *url.Error,
		// whose Error() is `Op "full URL": underlying error`, and these providers' credentials **are in the URL**
		// (DingTalk access_token, WeCom key, Feishu hook id, Telegram /bot<token>/).
		// Without redaction, the credentials would flow along this error string to four places: notification_deliveries
		// last_error (stored in plaintext), the delivery history endpoint's response (**bypassing channel config masking**),
		// server logs, and the 502 text the test-send endpoint returns to the frontend.
		return nil, fmt.Errorf("request failed: %s", redactTransportError(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// 429 (rate limit) and 408 (timeout) are worth retrying; other 4xx are config or permission problems, retrying is pointless.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("peer rate-limited or timed out (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("peer service error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(fmt.Errorf("peer rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet compresses the response body into a short single line for error messages. Responses may contain newlines and lots of whitespace,
// and putting them straight into last_error would break the delivery history page layout.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget compresses a delivery address to "scheme://host/…" for error messages.
//
// This is the package's only address redaction criterion, and it is deliberately **crude enough**: everything except scheme and host
// is dropped. The reason is that there is no "general and safe" way to tell which part of a URL is the credential:
//
//	DingTalk  credential in query          /robot/send?access_token=xxx
//	WeCom     credential in query          /cgi-bin/webhook/send?key=xxx
//	Feishu    credential in **last path segment** /open-apis/bot/v2/hook/<hook_id>
//	Telegram  credential in **middle path segment** /bot<token>/sendMessage
//
// "Keeping only the useful parts" would require per-channel patches, and missing any one provider means a credential leak.
// Keeping the host is enough for troubleshooting (DNS resolution failure, connection failure, bad certificate can all be located),
// and which specific bot it is can be recognized from the masked suffix hint in the channel config.
//
// On parse failure a fixed placeholder is returned — the raw string is never echoed back.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(unparseable address)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError strips the address from a transport-layer error, keeping only the underlying cause.
//
// *url.Error has the structure {Op, URL, Err}, and Error() prints the URL along with it.
// Here we take the Err field explicitly, bypassing its Error() — more reliable than string replacement after the fact,
// because replacement must correctly handle all the URL-encoded/escaped variants and easily misses some.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// Non-*url.Error errors (e.g. ones returned by the redirect policy) may also contain addresses; redact them uniformly.
	return redactURLsInText(err.Error())
}

// redactURLsInText replaces http(s) addresses appearing in a piece of text with their redacted form.
//
// Used as a fallback for errors where structured fields aren't available (redirect policy errors, custom errors from third-party libraries).
// Only http/https prefixes are recognized, split on whitespace and quotes — addresses don't contain these characters.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
