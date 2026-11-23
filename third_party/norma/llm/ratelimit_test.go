package llm

import (
	"context"
	"testing"
	"time"
)

func TestTokenBucketReservesRequestedAmount(t *testing.T) {
	b := newBucket(100, 100)
	if delay := b.reserveN(100); delay != 0 {
		t.Fatalf("initial capacity should be available immediately, got %s", delay)
	}
	delay := b.reserveN(10)
	if delay < 80*time.Millisecond || delay > 150*time.Millisecond {
		t.Fatalf("10 tokens at 100 tokens/second should wait about 100ms, got %s", delay)
	}
}

func TestTokensPerMinuteHonorsCancellation(t *testing.T) {
	limiter := newRateLimiter(RateLimit{TokensPerMinute: 60})
	if err := limiter.Wait(context.Background(), 60); err != nil {
		t.Fatalf("initial token budget: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := limiter.Wait(ctx, 1); err != context.DeadlineExceeded {
		t.Fatalf("expected deadline while waiting for exhausted TPM budget, got %v", err)
	}
}

func TestEstimateRequestTokensGrowsWithContext(t *testing.T) {
	small := estimateRequestTokens(CompletionRequest{Messages: []Message{UserText("short")}})
	large := estimateRequestTokens(CompletionRequest{Messages: []Message{UserText("a much longer prompt with more context")}})
	if small <= 0 || large <= small {
		t.Fatalf("expected a positive, context-sensitive estimate: small=%f large=%f", small, large)
	}
}
