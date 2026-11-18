package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type browserConfig struct {
	Headless    bool
	ProxyServer string
	// ProxyCA is the recording proxy's MITM CA. Chromium on Linux only trusts
	// CAs from an NSS database, which would need extra tooling to populate, so
	// when a proxy is configured the browser instead ignores certificate errors.
	// That is only enabled together with --proxy-server, i.e. when every request
	// already goes through Elenxa's own MITM proxy.
	ProxyCA   string
	Chromium  string
	OutputDir string
	NoSandbox bool
	// AllowVendorHosts lets the browser resolve Google service domains
	// (only needed if a test target is actually hosted on them).
	AllowVendorHosts bool
}

const callTimeout = 30 * time.Second

// browser owns one Chromium process and one page, started lazily on first use
// and restarted automatically if it dies.
type browser struct {
	cfg browserConfig

	mu      sync.Mutex
	cmd     *exec.Cmd
	exited  chan struct{} // closed when cmd has been reaped
	conn    *cdpConn
	session string
	dataDir string

	dmu     sync.Mutex
	dialogs []string
}

func newBrowser(cfg browserConfig) *browser {
	sweepStaleProfiles()
	return &browser{cfg: cfg}
}

const profilePrefix = "elenxa-browser-profile-"

// sweepStaleProfiles removes profile dirs left behind by an instance that was
// killed before it could clean up. A dir is only removed when its Chromium is
// gone (SingletonLock missing or pointing at a dead pid) and it is older than a
// minute, so profiles of other running MCP instances are never touched.
func sweepStaleProfiles() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), profilePrefix+"*"))
	for _, d := range dirs {
		fi, err := os.Lstat(d)
		if err != nil || !fi.IsDir() || time.Since(fi.ModTime()) < time.Minute {
			continue
		}
		if target, err := os.Readlink(filepath.Join(d, "SingletonLock")); err == nil {
			// target is "<hostname>-<pid>"
			if i := strings.LastIndexByte(target, '-'); i >= 0 {
				if pid, err := strconv.Atoi(target[i+1:]); err == nil && pid > 0 &&
					syscall.Kill(pid, 0) != syscall.ESRCH {
					continue // owner still alive (or not ours to judge)
				}
			}
		}
		_ = os.RemoveAll(d)
	}
}

func (b *browser) findChromium() (string, error) {
	if b.cfg.Chromium != "" {
		return b.cfg.Chromium, nil
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chromium binary found on PATH (install chromium or pass --chromium)")
}

// Features that make Chromium talk to Google on its own. Unknown names are ignored.
var disabledFeatures = []string{
	"Translate", "OptimizationHints", "OptimizationHintsFetching", "MediaRouter",
	"AutofillServerCommunication", "CertificateTransparencyComponentUpdater",
	"InterestFeedContentSuggestions", "NetworkTimeServiceQuerying", "SafeBrowsingRealTimeUrlLookup",
	"AIModeOmniboxEntryPoint", "LensOverlay", "GlicRollout",
	"PrivacySandboxSettings4", "BrowsingTopics", "FedCm", "PushMessaging", "DialMediaRouteProvider",
}

// Google service domains blocked at the browser's resolver (see --allow-vendor-hosts).
var vendorDomains = []string{
	"google.com", "googleapis.com", "gstatic.com", "gvt1.com", "gvt2.com",
	"googleusercontent.com", "google-analytics.com", "googletagmanager.com",
	"doubleclick.net", "googlesyndication.com", "youtube.com", "ytimg.com", "withgoogle.com",
}

func vendorResolverRules() string {
	var r []string
	for _, d := range vendorDomains {
		r = append(r, "MAP "+d+" ~NOTFOUND", "MAP *."+d+" ~NOTFOUND")
	}
	return strings.Join(r, ", ")
}

func proxyBypassList(allowVendor bool) string {
	l := []string{"<-loopback>"}
	if !allowVendor {
		for _, d := range vendorDomains {
			l = append(l, d, "*."+d)
		}
	}
	return strings.Join(l, ";")
}

func (b *browser) args() []string {
	a := []string{
		"--remote-debugging-pipe",
		"--user-data-dir=" + b.dataDir,
		"--crash-dumps-dir=" + filepath.Join(b.dataDir, ".crash"),
		"--no-first-run",
		"--no-default-browser-check",
		// Keep the browser from contacting Google services on its own.
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-sync",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-domain-reliability",
		"--disable-client-side-phishing-detection",
		"--safebrowsing-disable-auto-update",
		"--disable-breakpad",
		"--metrics-recording-only",
		"--no-pings",
		"--disable-features=" + strings.Join(disabledFeatures, ","),
		"--password-store=basic",
		"--use-mock-keychain",
		"--disable-dev-shm-usage",
		"--window-size=1280,900",
	}
	if b.cfg.Headless {
		a = append(a, "--headless=new", "--hide-scrollbars", "--mute-audio")
	}
	if b.cfg.NoSandbox {
		a = append(a, "--no-sandbox")
	}
	if !b.cfg.AllowVendorHosts {
		// Backstop for anything the flags above and the managed policy miss:
		// the browser cannot resolve Google's service hostnames at all. (When a
		// proxy is set, the proxy resolves names instead; the policy file and
		// the egress firewall cover that case.)
		a = append(a, "--host-resolver-rules="+vendorResolverRules())
	}
	if b.cfg.ProxyServer != "" {
		a = append(a, "--proxy-server="+b.cfg.ProxyServer,
			// Send loopback targets through the recording proxy too. Google service
			// domains bypass the proxy, i.e. go "direct", where the resolver rules
			// above make them fail, so the proxy never fetches them on our behalf.
			"--proxy-bypass-list="+proxyBypassList(b.cfg.AllowVendorHosts),
			"--ignore-certificate-errors")
	}
	return append(a, "about:blank")
}

// ensure starts Chromium if it isn't running. Callers hold no locks.
func (b *browser) ensure(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && !b.conn.isClosed() {
		return nil
	}
	b.stopLocked()

	bin, err := b.findChromium()
	if err != nil {
		return err
	}
	if b.dataDir, err = os.MkdirTemp("", profilePrefix); err != nil {
		return err
	}
	toChromeR, toChromeW, err := os.Pipe()
	if err != nil {
		return err
	}
	fromChromeR, fromChromeW, err := os.Pipe()
	if err != nil {
		toChromeR.Close()
		toChromeW.Close()
		return err
	}
	cmd := exec.Command(bin, b.args()...)
	cmd.ExtraFiles = []*os.File{toChromeR, fromChromeW} // fd 3 = commands in, fd 4 = replies out
	cmd.Stdout = io.Discard
	errTail := &tailBuffer{max: 4096} // last few KB of Chromium's stderr, for startup errors
	cmd.Stderr = errTail
	cmd.Env = childEnv(b.dataDir)
	// Own process group, so stopLocked can signal Chromium's whole tree (zygote,
	// renderers, crashpad); Pdeathsig kills it if this server dies without
	// cleaning up (its helpers then exit when their parent goes away).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		toChromeR.Close()
		toChromeW.Close()
		fromChromeR.Close()
		fromChromeW.Close()
		return fmt.Errorf("start chromium: %w", err)
	}
	toChromeR.Close()
	fromChromeW.Close()
	b.cmd = cmd
	conn := newCDPConn(toChromeW, fromChromeR, b.onEvent)
	exited := make(chan struct{})
	b.conn, b.exited = conn, exited
	// Capture conn/exited: stopLocked may clear the fields before Chromium exits.
	go func() { _ = cmd.Wait(); conn.close(); close(exited) }()

	sctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := b.attachLocked(sctx); err != nil {
		hint := ""
		if !b.cfg.NoSandbox {
			hint = " (if Chromium cannot start its sandbox here, pass --no-sandbox)"
		}
		b.stopLocked()
		if t := errTail.String(); t != "" {
			hint += "\nchromium stderr (tail):\n" + t
		}
		return fmt.Errorf("connect to chromium: %w%s", err, hint)
	}
	log.Printf("chromium started (%s)", bin)
	return nil
}

func (b *browser) attachLocked(ctx context.Context) error {
	raw, err := b.conn.call(ctx, "", "Target.getTargets", nil)
	if err != nil {
		return err
	}
	var tg struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	_ = json.Unmarshal(raw, &tg)
	targetID := ""
	for _, t := range tg.TargetInfos {
		if t.Type == "page" {
			targetID = t.TargetID
			break
		}
	}
	if targetID == "" {
		raw, err := b.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
		if err != nil {
			return err
		}
		var ct struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(raw, &ct)
		targetID = ct.TargetID
	}
	raw, err = b.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		return err
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(raw, &at)
	if at.SessionID == "" {
		return errors.New("no session id from attachToTarget")
	}
	b.session = at.SessionID
	if _, err := b.conn.call(ctx, b.session, "Page.enable", nil); err != nil {
		return err
	}
	return nil
}

// onEvent runs on the CDP reader goroutine: it must not wait on CDP replies.
func (b *browser) onEvent(conn *cdpConn, method, session string, params json.RawMessage) {
	if method != "Page.javascriptDialogOpening" {
		return
	}
	var d struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		URL     string `json:"url"`
	}
	_ = json.Unmarshal(params, &d)
	b.dmu.Lock()
	b.dialogs = append(b.dialogs, fmt.Sprintf("%s(%q) on %s", d.Type, d.Message, d.URL))
	b.dmu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Accept so page scripts don't stay blocked; the dialog is reported to the agent.
		_, _ = conn.call(ctx, session, "Page.handleJavaScriptDialog", map[string]any{"accept": true})
	}()
}

// takeDialogs returns and clears the dialogs seen since the last call.
func (b *browser) takeDialogs() []string {
	b.dmu.Lock()
	defer b.dmu.Unlock()
	d := b.dialogs
	b.dialogs = nil
	return d
}

// page sends a command to the attached page session, starting the browser if needed.
func (b *browser) page(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	conn, sess := b.conn, b.session
	b.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return conn.call(cctx, sess, method, params)
}

// eval runs JS in the page and returns its JSON value.
func (b *browser) eval(ctx context.Context, expr string) (json.RawMessage, error) {
	raw, err := b.page(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
		"userGesture":   true,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		msg := r.ExceptionDetails.Exception.Description
		if msg == "" {
			msg = r.ExceptionDetails.Text
		}
		return nil, fmt.Errorf("javascript error: %s", msg)
	}
	return r.Result.Value, nil
}

// waitReady polls document.readyState until "complete" or the timeout passes.
func (b *browser) waitReady(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v, err := b.eval(ctx, "document.readyState")
		if err == nil && strings.Trim(string(v), `"`) == "complete" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (b *browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
}

func (b *browser) stopLocked() {
	if b.conn != nil && !b.conn.isClosed() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = b.conn.call(ctx, "", "Browser.close", nil)
		cancel()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		// Terminate the whole process group, then make sure it is gone before
		// deleting the profile: helpers still running would keep writing into it.
		pgid := b.cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		if !waitOrTimeout(b.exited, 2*time.Second) {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			waitOrTimeout(b.exited, 2*time.Second)
		}
		// The leader may be reaped before its children; sweep the group once more.
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	if b.conn != nil {
		b.conn.close()
	}
	if b.dataDir != "" {
		_ = os.RemoveAll(b.dataDir)
	}
	b.cmd, b.exited, b.conn, b.session, b.dataDir = nil, nil, nil, "", ""
}

func waitOrTimeout(ch <-chan struct{}, d time.Duration) bool {
	if ch == nil {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	}
}

// childEnv gives Chromium only what it needs, never Elenxa's secrets.
//
// HOME and the XDG config/cache dirs point into the throwaway profile dir, so
// Chromium (and its crashpad handler, which refuses to start without a writable
// database dir) works when the real home is read-only — as in the container — and
// leaves nothing behind outside the profile, which is deleted on close.
func childEnv(profile string) []string {
	keep := map[string]bool{"PATH": true, "USER": true, "LANG": true, "TZ": true,
		"TMPDIR": true, "XDG_RUNTIME_DIR": true, "FONTCONFIG_PATH": true}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if keep[k] || strings.HasPrefix(k, "LC_") {
			out = append(out, kv)
		}
	}
	return append(out,
		"HOME="+profile,
		"XDG_CONFIG_HOME="+filepath.Join(profile, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(profile, ".cache"))
}

// ---- DevTools protocol over the pipe (messages are NUL-terminated JSON) ----

type cdpReply struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type cdpConn struct {
	w       io.WriteCloser
	r       io.ReadCloser
	onEvent func(c *cdpConn, method, session string, params json.RawMessage)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan cdpReply
	closed  chan struct{}
	once    sync.Once
}

func newCDPConn(w io.WriteCloser, r io.ReadCloser, onEvent func(*cdpConn, string, string, json.RawMessage)) *cdpConn {
	c := &cdpConn{w: w, r: r, onEvent: onEvent, pending: map[int64]chan cdpReply{}, closed: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *cdpConn) readLoop() {
	br := bufio.NewReaderSize(c.r, 1<<20)
	for {
		frame, err := br.ReadBytes(0)
		if err != nil {
			c.close()
			return
		}
		frame = frame[:len(frame)-1]
		var m cdpReply
		if json.Unmarshal(frame, &m) != nil {
			continue
		}
		if m.ID == 0 {
			if m.Method != "" && c.onEvent != nil {
				c.onEvent(c, m.Method, m.SessionID, m.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

func (c *cdpConn) call(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	ch := make(chan cdpReply, 1)
	c.mu.Lock()
	if c.isClosed() {
		c.mu.Unlock()
		return nil, errors.New("browser is not running")
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	data, err := json.Marshal(msg)
	if err == nil {
		_, err = c.w.Write(append(data, 0))
	}
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	case <-c.closed:
		return nil, errors.New("browser exited")
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *cdpConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *cdpConn) close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.w.Close()
		_ = c.r.Close()
	})
}

// tailBuffer is an io.Writer that keeps only the last max bytes written.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if over := len(t.b) - t.max; over > 0 {
		t.b = append(t.b[:0], t.b[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.b))
}
