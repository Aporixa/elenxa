package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// This file covers doJSON's HTTP-layer error classification.
//
// Why test it separately: each channel adapter handles only the platform's own business error codes (DingTalk errcode,
// Feishu code, Telegram ok field), while the **HTTP-layer** classification is done uniformly by doJSON;
// the two are independent lines of defense. Without this one, a relay gateway returning 503 would be treated as a permanent failure
// and retries abandoned; and a 403 would be treated as retryable, backing off three rounds for nothing.

func replyServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoJSONClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"200 success is not an error", 200, false},
		{"429 rate limit retryable", 429, false},
		{"408 request timeout retryable", 408, false},
		{"500 server error retryable", 500, false},
		{"502 gateway error retryable", 502, false},
		{"503 service unavailable retryable", 503, false},
		{"400 bad parameters permanent failure", 400, true},
		{"401 auth failed permanent failure", 401, true},
		{"403 forbidden permanent failure", 403, true},
		{"404 address not found permanent failure", 404, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := replyServer(t, tc.status, `{"detail":"upstream says no"}`)
			_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
			if tc.status < 300 {
				if err != nil {
					t.Fatalf("2xx should not return an error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("non-2xx should return an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("wrong permanent verdict for HTTP %d: expected %v got %v (%v)",
					tc.status, tc.permanent, got, err)
			}
			// The status code must appear in the error, otherwise the user can't tell whether they misconfigured or the peer is down.
			// Assert on the number rather than Go's English StatusText: this package's copy was originally Chinese
			// (consistent with the rest of the project), and the number is the language-independent, stably assertable part.
			if !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Errorf("error message should include HTTP status code %d, got %v", tc.status, err)
			}
		})
	}
}

// TestDoJSONIncludesResponseSnippet covers the snippet: the peer's error explanation must be brought back,
// otherwise the user only knows "it failed", not why the peer rejected it.
func TestDoJSONIncludesResponseSnippet(t *testing.T) {
	srv := replyServer(t, 400, `{"error":"invalid webhook token"}`)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("should return an error")
	}
	if !strings.Contains(err.Error(), "invalid webhook token") {
		t.Errorf("error message should bring back the peer's explanation, got %v", err)
	}
}

// TestDoJSONSnippetIsSingleLineAndBounded constrains the snippet's shape:
// the peer response goes as-is into the last_error column and the frontend table; multi-line/overlong content breaks layout and payloads.
func TestDoJSONSnippetIsSingleLineAndBounded(t *testing.T) {
	// A response with newlines, tabs and 5000 characters of overlong content.
	long := strings.Repeat("x", 5000)
	srv := replyServer(t, 500, "line1\nline2\r\n\tline3 "+long)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("should return an error")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n\t") {
		t.Errorf("error message should be compressed to a single line, got %q", msg)
	}
	// snippet limit of 200 characters + fixed prefix; the total must be far smaller than the raw response.
	if len(msg) > 400 {
		t.Errorf("error message too long (%d bytes), should be truncated by snippet: %q", len(msg), msg)
	}
}

// TestDoJSONRejectsOversizedResponse confirms reading has a limit: when the peer abnormally returns huge content,
// the whole response must not be read into memory (every entry in the delivery history stores a copy of last_error).
func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) // 1 MiB
	srv := replyServer(t, 400, huge)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("should return an error")
	}
	if len(err.Error()) > 400 {
		t.Errorf("oversized response should be read with a limit and truncated, error message length %d", len(err.Error()))
	}
}
