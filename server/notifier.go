package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Aporixa/elenxa/db"
	"github.com/Aporixa/elenxa/notify"
)

// Global settings keys (stored in the settings key-value table, no dedicated table needed).
const (
	// settingNotifyEnabled is the master switch for push notifications. On by default: it exists as a one-click kill switch during maintenance,
	// not as the feature's enable condition - the real enable condition is "is any channel configured".
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL is the external address used to build deep links to finding details
	// (e.g. https://elenxa.example.com). If empty, messages carry no deep-link button.
	// The project had no reusable external-address setting, so a new one is added here.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes is the digest-mode interval (minutes).
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick is the delivery engine's polling interval. 3 seconds is the engine's latency floor,
	// and the main source of delay between "finding persisted" and "message arrives in IM".
	notifyTick = 3 * time.Second
	// notifyLease is the lease duration when claiming deliveries. It must be well above the worst-case time of a single delivery
	// (the notify package's HTTP client timeout is 15 seconds), otherwise the same row can be delivered by two
	// dispatchers at once.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick caps events fanned out per tick, so that enabling a channel for the first time
	// doesn't expand the entire historical backlog into delivery jobs at once.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest interval.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick is the per-tick delivery cap for channels with no rate limit.
	// It exists to stop "a channel set to unlimited + a scan producing thousands of findings" from turning
	// a single loop iteration into a long block.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick is the maximum number of deliveries per channel per tick.
	//
	// This cap is derived backwards from the **lease duration**: claimed rows get a lease (notifyLease = 3 minutes),
	// and if a tick delivers so many items serially that the worst case exceeds the lease, the lease expires before the last few are sent.
	// Within one process this doesn't matter (Run is a single serial goroutine, ticks don't re-enter), but when **two
	// processes share one database**, the other side re-claims rows with expired leases and sends them again, and also
	// double-increments attempts, marking them failed while the original process is still delivering.
	//
	// Value: 3-minute lease / 30-second per-send timeout = 6 **uses the lease exactly**, with zero margin,
	// so it's not allowed; 5 gives a worst case of 150 seconds with 30 seconds of margin. This relationship is pinned by
	// TestNotifyTickBudgetFitsWithinLease - changing notifyLease,
	// notifySendTimeout or this value will make that assertion fail.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout is the timeout for a single delivery. It also determines the previous constant's value;
	// their product must not exceed notifyLease, see TestNotifyTickBudgetFitsWithinLease.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is the backoff sequence for failed retries, indexed by attempts made.
// 3 chances (including the first) correspond to db.MaxNotifyAttempts; the two must change together.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Notifier is the delivery engine for finding push notifications.
//
// It runs alongside Scheduler as an independent goroutine (see server.New). It deliberately doesn't reuse
// Scheduler's tick: push latency requirements (3 seconds) differ from the triggers' business cadence,
// and failures in one must not affect the other - a stuck push should not affect agent triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects buckets. Few channels and low contention, so one mutex is enough;
	// not worth introducing a finer-grained structure for it.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is a per-channel token bucket.
//
// A token bucket is used instead of a "count per minute then reset" window because the latter has bad edge effects:
// sending 20 at the end of a window and 20 more the next instant is 40 within one second to the platform,
// which gets rate limited; a token bucket refills at a constant rate and naturally avoids such bursts.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until ctx is done. Started once by server.New.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step runs one round: first fan out new events, then deliver due jobs.
//
// Any failed step is only logged and never breaks the loop - a notification failure must never escalate into a process-level problem.
// Each tick is independent; the next round naturally retries.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf("[notify] fan out events failed: %v", err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf("[notify] read channels failed: %v", err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// The token bucket counts **messages** (equivalent to HTTP requests), not findings.
		// In realtime mode they're the same (one finding, one message); in digest mode a whole batch of findings is merged into
		// one message, so it consumes only one token.
		//
		// Both modes ask the token bucket first, then claim within the quota - the order must not be reversed, otherwise deliveries blocked by the rate limit
		// would already have consumed retry attempts.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns a digest channel's token cost and batch-size upper bound for this tick.
//
// The two return values are **two different units**, which is exactly why this is its own function:
//
//   - tokens is a message count. A batch of findings becomes one message and one HTTP request, so it's always 1.
//     rate_per_min therefore still applies to digest (at most that many digest messages per minute).
//   - claimLimit is the maximum number of findings in this batch. It's bounded only by memory, unrelated to the request budget.
//
// Previously, to make rate_per_min apply to digest, the per-tick request budget
// (notifyMaxSendsPerChannelPerTick, derived from the lease) was passed down directly as the batch size.
// As a result a channel with rate_per_min=20 only refilled 1 token per 3-second tick, so every digest
// message held just 1 finding - digest degraded into "realtime push with digest wording", and readers got a string of
// "1 new finding in the last 30 minutes", while db.MaxDigestBatchSize was never reachable.
//
// This symptom is hard to catch in end-to-end tests (existing cases all pass a large enough limit to
// stepDigest manually, bypassing the quota calculation in step), so the decision is collected here and pinned directly by
// TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and delivers a channel's realtime jobs, one message per finding.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] claim realtime deliveries failed channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel kind %q is not registered", ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// A render failure is a local data problem; retrying won't help.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, err.Error())
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest aggregates a channel's pending deliveries into one message and sends it when the batch is due.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf("[notify] check digest batch failed channel=%d: %v", ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf("[notify] claim digest batch failed channel=%d: %v", ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf("channel kind %q is not registered", ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), err.Error())
		return
	}
	// Deliveries whose snapshot is broken and didn't make it into the message must be explicitly failed. Otherwise they'd stay
	// outside included, in neither the message nor the failure list - on a successful send their state would be
	// missed by the subsequent bulk marking, stuck in sending forever and re-claimed each time the lease expires.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := "event snapshot could not be parsed; this finding cannot be rendered into a message"
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf("[notify] failed to mark broken-snapshot deliveries as failed channel=%s ids=%v: %v", ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf("[notify] skipped %d deliveries with unparseable snapshots channel=%d", len(skipped), ch.ID)
	}
	// Only pass the ones in the message to send: included[i] corresponds strictly to msg.Items[i],
	// and send relies on this to apply "the channel reported it fit the first K" to the right delivery rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers and transitions state based on the result.
//
// A batch of deliveries (possibly dozens in digest mode) shares one send result: either delivered, or the whole batch is retried.
// No per-item retry - a digest is one message, and resending part of it would break batch semantics.
//
// The only exception is **segmentation caused by the channel's length limit**: the channel reports it actually fit only the first K,
// so items from K+1 on must wait for the next batch instead of being marked successful along with them. Otherwise the truncated
// findings are in neither the message nor the failure list, and vanish entirely.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	// Cap a single delivery so one stuck channel doesn't hold up all remaining channels this round.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// The count reported by the channel can't exceed the number of deliveries; if it does, the render layer miscounted,
			// so treat all as delivered and log the problem - better than corrupting the records.
			log.Printf("[notify] channel reported %d delivered, more than %d deliveries channel=%s; treating all as delivered",
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf("[notify] mark delivered failed channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// This message hit the channel's length limit: the rest go straight back to the queue for the next tick to continue.
			// Use DeferDeliveries rather than RescheduleDeliveries - this isn't a failure
			// and shouldn't consume retry budget (claiming already optimistically did +1; that gets subtracted back there).
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf("message hit the channel length limit; only the first %d were delivered, the rest are left for the next batch", delivered)); err != nil {
				log.Printf("[notify] failed to requeue remaining segments channel=%s ids=%v: %v", channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// The channel neither returned an error nor said how many were delivered. Treat as failure (with backoff),
		// so this delivery isn't claimed over and over without ever being marked.
		err = fmt.Errorf("channel did not report a delivered count (delivered=%d)", delivered)
	}

	// Failure handling is decided **per item**, not by the maximum attempts across the whole batch.
	//
	// It used to be `if maxAttempts(deliveries) >= MaxNotifyAttempts` failing the whole batch, but items in a batch
	// have different attempt counts: an old delivery already retried twice (attempts=2) would drag
	// brand-new deliveries (attempts=1) in the same batch into failed - new findings were lost permanently without a single retry,
	// the exact opposite of the intent "don't let old rows drag new rows down".
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, err.Error()); fErr != nil {
			log.Printf("[notify] error marking failed state channel=%s ids=%v: %v", channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf("still failing after %d retries: %s", db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf("[notify] error marking failed state channel=%s ids=%v: %v", channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Reschedule grouped by delay: there are only 3 backoff tiers so the group count is naturally small; no need for a separate
	// UPDATE per item (that would make a 500-item batch cost 500 round trips).
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, err.Error()); rErr != nil {
			log.Printf("[notify] reschedule deliveries failed channel=%s ids=%v: %v", channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf("[notify] delivery failed channel=%d kind=%s permanent=%d exhausted=%d retrying=%d: %s",
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries returns those in all that are not in keep (compared by pointer identity).
// Used to find deliveries that "didn't make it into the message" - they must be handled explicitly, not left in limbo.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt looks up the channel implementation and parses its config.
// ok=false means the kind isn't registered; the delivery should fail outright rather than retry forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// On config parse failure, use an empty map: the channel's own Validate will report "which field is missing",
		// an error that guides the user to a fix better than a JSON parse error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders a single-finding message.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL}, nil
}

// renderBatch renders a digest message. Snapshots are parsed one by one - a single broken one is skipped
// rather than taking down the whole digest.
//
// The returned included corresponds **strictly one-to-one** with msg.Items (i-th delivery ↔ i-th item).
// This correspondence is a hard requirement: the caller uses "the channel reported it fit the first K" to mark the first K deliveries
// as delivered. If a broken snapshot were skipped here without removing that delivery from included,
// indexes would shift - broken items that should fail would be marked delivered, and good items misjudged as undelivered.
// Broken ones are explicitly marked failed by the caller, see stepDigest.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// A broken snapshot goes into neither the message nor included - the caller is responsible for handling it
			// (explicitly marking it failed, rather than slipping it through among "delivered").
			log.Printf("[notify] skipped unparseable snapshot in digest batch delivery=%d: %v", dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, fmt.Errorf("none of the %d deliveries in the digest batch could be parsed", len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders an event snapshot into a push item, also resolving the asset name and detail deep link.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Failing to resolve the asset name shouldn't block the push: a missing name is far less bad than a missing notification,
		// the message just lacks one asset line.
		log.Printf("[notify] resolve asset name failed finding=%d: %v", snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// For the detail page route see web/src/app/(main)/function/findings/detail/page.tsx;
		// it reads the finding id from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes **at most want** tokens from the channel's bucket and returns how many it actually got.
//
// One token = one message (one HTTP request). In realtime mode the caller passes however many it wants;
// in digest mode a whole batch of findings is one message, so it passes 1.
//
// Bucket capacity is the channel's per-minute limit, refilled at a constant rate. ratePerMin<=0 means unlimited,
// returning a finite but large enough value to keep a single round from being held up by an unbounded backlog.
//
// The want cap is necessary: without it the bucket could only be drained entirely, and since the caller has its own per-tick cap,
// the extra tokens would be unused and vanish before the next refill - the accumulated burst capacity would never be reachable,
// and even "no pending deliveries this round" would still be charged.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Refill by real elapsed time, at ratePerMin/60 per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before truncating: the token count is accumulated in floating point, so refilling in two steps
	// 0.5 + 0.5 can yield 0.9999999999, and a plain int() truncates it to 0 -
	// a mathematically full bucket that yields no token. 1e-9 is far smaller than one token and won't let a real shortfall through.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the master switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL returns the external address for deep links, with the trailing slash removed.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval returns the digest interval, falling back to the default when invalid or unset.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot parses the snapshot of the event behind a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, fmt.Errorf("event snapshot for delivery %d is empty", dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, fmt.Errorf("parse event snapshot for delivery %d failed: %w", dl.ID, err)
	}
	if snap.Kind == "" {
		// The event type comes from the event row; the copy in the snapshot may have been written by an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
