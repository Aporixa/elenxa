package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const defaultMaxChars = 20000

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	run         func(ctx context.Context, args map[string]any) (string, error)
}

type toolset struct {
	b     *browser
	tools []toolDef
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

var target = map[string]any{
	"ref":      str("Element ref from the latest browser_snapshot, e.g. e12"),
	"selector": str("CSS selector (used when ref is not given)"),
}

func withTarget(extra map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range target {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func newToolset(b *browser) *toolset {
	t := &toolset{b: b}
	t.tools = []toolDef{
		{"browser_navigate", "Open a URL (http/https) in the browser and wait for it to load. Returns the final URL and title.",
			obj(map[string]any{"url": str("Absolute http(s) URL")}, "url"), t.navigate},
		{"browser_snapshot", "Describe the current page: URL, title, forms, interactive elements with refs (e1, e2, ...) for click/type, and visible text. Take a new snapshot after the page changes; refs are reassigned each time.",
			obj(map[string]any{"max_chars": num("Max characters of page text (default 20000)")}), t.snapshot},
		{"browser_click", "Click an element (real mouse events at its center).",
			obj(withTarget(nil)), t.click},
		{"browser_type", "Focus an input/textarea and type text, optionally clearing it first and pressing Enter after.",
			obj(withTarget(map[string]any{
				"text":   str("Text to type"),
				"clear":  boolean("Clear the current value first (default true)"),
				"submit": boolean("Press Enter after typing"),
			}), "text"), t.typeText},
		{"browser_select_option", "Select option(s) in a <select> by value or visible text.",
			obj(withTarget(map[string]any{"values": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}), "values"), t.selectOption},
		{"browser_press_key", "Press a key on the focused element: Enter, Tab, Escape, Backspace, Delete, Space, ArrowUp/Down/Left/Right, Home, End, PageUp, PageDown, or a single character.",
			obj(map[string]any{"key": str("Key name")}, "key"), t.pressKey},
		{"browser_back", "Go back one page in history.", obj(map[string]any{}), t.back},
		{"browser_wait_for", "Wait until text appears on the page, or just wait a number of seconds (max 30).",
			obj(map[string]any{"text": str("Text to wait for"), "seconds": num("Seconds to wait / timeout (default 10)")}), t.waitFor},
		{"browser_evaluate", "Run a JavaScript expression in the page and return its JSON result (promises are awaited).",
			obj(map[string]any{"expression": str("JavaScript expression")}, "expression"), t.evaluate},
		{"browser_get_html", "Return outerHTML of the page or of one element.",
			obj(withTarget(map[string]any{"max_chars": num("Max characters (default 20000)")})), t.getHTML},
		{"browser_get_cookies", "List cookies visible to the current page, including HttpOnly ones.", obj(map[string]any{}), t.cookies},
		{"browser_screenshot", "Save a PNG screenshot to disk and return its path.",
			obj(map[string]any{"full_page": boolean("Capture the full scrollable page"), "filename": str("Optional file name (.png)")}), t.screenshot},
		{"browser_close", "Close the browser. The next browser tool call starts a fresh one with empty cookies/storage.", obj(map[string]any{}), t.closeBrowser},
	}
	return t
}

func (t *toolset) list() []toolDef { return t.tools }

func (t *toolset) call(ctx context.Context, name string, raw json.RawMessage) (string, bool) {
	args := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "Error: arguments must be a JSON object", true
		}
	}
	for _, td := range t.tools {
		if td.Name == name {
			out, err := td.run(ctx, args)
			if d := t.b.takeDialogs(); len(d) > 0 {
				out += "\nDialogs opened (auto-accepted): " + strings.Join(d, "; ")
			}
			if err != nil {
				return "Error: " + err.Error() + out, true
			}
			return out, false
		}
	}
	return "Error: unknown tool " + name, true
}

// ---- argument helpers ----

func sArg(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}

func bArg(a map[string]any, k string, def bool) bool {
	if v, ok := a[k].(bool); ok {
		return v
	}
	return def
}

func nArg(a map[string]any, k string, def float64) float64 {
	if v, ok := a[k].(float64); ok && v > 0 {
		return v
	}
	return def
}

func jsq(s string) string { b, _ := json.Marshal(s); return string(b) }

// elementJS returns a JS expression evaluating to the target element (or null).
func elementJS(a map[string]any) (string, error) {
	if ref := sArg(a, "ref"); ref != "" {
		return "document.querySelector('[data-elenxa-ref=' + JSON.stringify(" + jsq(ref) + ") + ']')", nil
	}
	if sel := sArg(a, "selector"); sel != "" {
		return "document.querySelector(" + jsq(sel) + ")", nil
	}
	return "", fmt.Errorf("give ref (from browser_snapshot) or selector")
}

func (t *toolset) pageSummary(ctx context.Context) string {
	v, err := t.b.eval(ctx, "JSON.stringify({u: location.href, t: document.title})")
	if err != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	var p struct{ U, T string }
	_ = json.Unmarshal([]byte(s), &p)
	return fmt.Sprintf("URL: %s\nTitle: %s", p.U, p.T)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n... [truncated, %d more characters]", len(s)-n)
}

// ---- tools ----

func (t *toolset) navigate(ctx context.Context, a map[string]any) (string, error) {
	raw := sArg(a, "url")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("url must be an absolute http(s) URL")
	}
	res, err := t.b.page(ctx, "Page.navigate", map[string]any{"url": u.String()})
	if err != nil {
		return "", err
	}
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(res, &nav)
	if nav.ErrorText != "" {
		return "", fmt.Errorf("navigation failed: %s", nav.ErrorText)
	}
	t.b.waitReady(ctx, 15*time.Second)
	return t.pageSummary(ctx), nil
}

const snapshotJS = `(() => {
  const max = %d;
  document.querySelectorAll('[data-elenxa-ref]').forEach(e => e.removeAttribute('data-elenxa-ref'));
  const sel = 'a[href],button,input,select,textarea,summary,[role=button],[role=link],[role=checkbox],[role=tab],[role=menuitem],[onclick],[contenteditable=""],[contenteditable=true]';
  const vis = e => { const r = e.getBoundingClientRect(); const s = getComputedStyle(e);
    return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; };
  const clean = s => String(s || '').trim().replace(/\s+/g, ' ').slice(0, 80);
  const els = []; let n = 0;
  for (const e of document.querySelectorAll(sel)) {
    if (e.type === 'hidden' || !vis(e)) continue;
    const ref = 'e' + (++n); e.setAttribute('data-elenxa-ref', ref);
    const d = {ref, tag: e.tagName.toLowerCase()};
    if (e.type && e.tagName !== 'BUTTON') d.type = e.type;
    if (e.name) d.name = e.name;
    if (e.id) d.id = e.id;
    const label = clean(e.getAttribute('aria-label') || e.innerText || e.placeholder || e.title || e.alt);
    if (label) d.label = label;
    if (e.href) d.href = e.href;
    if (e.tagName === 'INPUT' || e.tagName === 'TEXTAREA') d.value = e.type === 'password' ? (e.value ? '(set)' : '') : clean(e.value);
    if (e.checked) d.checked = true;
    if (e.disabled) d.disabled = true;
    if (e.tagName === 'SELECT') d.options = [...e.options].slice(0, 30).map(o => (o.selected ? '*' : '') + o.value);
    els.push(d);
  }
  const forms = [...document.forms].map(f => ({action: f.action, method: (f.getAttribute('method') || 'get').toUpperCase()}));
  const text = (document.body && document.body.innerText) || '';
  return {url: location.href, title: document.title, forms, els, text: text.slice(0, max), more: Math.max(0, text.length - max)};
})()`

func (t *toolset) snapshot(ctx context.Context, a map[string]any) (string, error) {
	max := int(nArg(a, "max_chars", defaultMaxChars))
	v, err := t.b.eval(ctx, fmt.Sprintf(snapshotJS, max))
	if err != nil {
		return "", err
	}
	var s struct {
		URL, Title string
		Forms      []struct{ Action, Method string }
		Els        []map[string]any
		Text       string
		More       int
	}
	if err := json.Unmarshal(v, &s); err != nil {
		return "", fmt.Errorf("bad snapshot: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\nTitle: %s\n", s.URL, s.Title)
	for i, f := range s.Forms {
		fmt.Fprintf(&b, "Form %d: %s %s\n", i+1, f.Method, f.Action)
	}
	b.WriteString("\nInteractive elements:\n")
	for _, e := range s.Els {
		fmt.Fprintf(&b, "[%v] %v", e["ref"], e["tag"])
		for _, k := range []string{"type", "name", "id", "label", "value", "href", "options", "checked", "disabled"} {
			if val, ok := e[k]; ok {
				fmt.Fprintf(&b, " %s=%s", k, compact(val))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\nPage text:\n")
	b.WriteString(s.Text)
	if s.More > 0 {
		fmt.Fprintf(&b, "\n... [truncated, %d more characters]", s.More)
	}
	return b.String(), nil
}

func compact(v any) string {
	if s, ok := v.(string); ok {
		return jsq(s)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func (t *toolset) click(ctx context.Context, a map[string]any) (string, error) {
	el, err := elementJS(a)
	if err != nil {
		return "", err
	}
	v, err := t.b.eval(ctx, `(() => { const e = `+el+`; if (!e) return null;
		e.scrollIntoView({block: 'center', inline: 'center'});
		const r = e.getBoundingClientRect(); return {x: r.left + r.width / 2, y: r.top + r.height / 2}; })()`)
	if err != nil {
		return "", err
	}
	var pt *struct{ X, Y float64 }
	_ = json.Unmarshal(v, &pt)
	if pt == nil {
		return "", fmt.Errorf("element not found (take a new browser_snapshot)")
	}
	for _, typ := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		p := map[string]any{"type": typ, "x": pt.X, "y": pt.Y}
		if typ != "mouseMoved" {
			p["button"] = "left"
			p["clickCount"] = 1
		}
		if _, err := t.b.page(ctx, "Input.dispatchMouseEvent", p); err != nil {
			return "", err
		}
	}
	time.Sleep(500 * time.Millisecond)
	t.b.waitReady(ctx, 10*time.Second)
	return "Clicked.\n" + t.pageSummary(ctx), nil
}

func (t *toolset) typeText(ctx context.Context, a map[string]any) (string, error) {
	el, err := elementJS(a)
	if err != nil {
		return "", err
	}
	clear := "false"
	if bArg(a, "clear", true) {
		clear = "true"
	}
	v, err := t.b.eval(ctx, `(() => { const e = `+el+`; if (!e) return false; e.scrollIntoView({block: 'center'}); e.focus();
		if (`+clear+` && 'value' in e) { e.value = ''; e.dispatchEvent(new Event('input', {bubbles: true})); }
		return true; })()`)
	if err != nil {
		return "", err
	}
	if string(v) != "true" {
		return "", fmt.Errorf("element not found (take a new browser_snapshot)")
	}
	if _, err := t.b.page(ctx, "Input.insertText", map[string]any{"text": sArg(a, "text")}); err != nil {
		return "", err
	}
	if bArg(a, "submit", false) {
		if err := t.key(ctx, "Enter"); err != nil {
			return "", err
		}
		time.Sleep(500 * time.Millisecond)
		t.b.waitReady(ctx, 10*time.Second)
		return "Typed and submitted.\n" + t.pageSummary(ctx), nil
	}
	return "Typed.", nil
}

func (t *toolset) selectOption(ctx context.Context, a map[string]any) (string, error) {
	el, err := elementJS(a)
	if err != nil {
		return "", err
	}
	vals, _ := json.Marshal(a["values"])
	v, err := t.b.eval(ctx, `(() => { const e = `+el+`; if (!e || e.tagName !== 'SELECT') return null;
		const want = `+string(vals)+` || []; const hit = o => want.includes(o.value) || want.includes(o.text.trim());
		if (e.multiple) { for (const o of e.options) o.selected = hit(o); }
		else { const o = [...e.options].find(hit); if (!o) return []; e.value = o.value; }
		e.dispatchEvent(new Event('input', {bubbles: true})); e.dispatchEvent(new Event('change', {bubbles: true}));
		return [...e.selectedOptions].map(o => o.value); })()`)
	if err != nil {
		return "", err
	}
	if string(v) == "null" {
		return "", fmt.Errorf("select element not found")
	}
	return "Selected: " + string(v), nil
}

var keyCodes = map[string]struct {
	code string
	vk   int
	text string
}{
	"Enter": {"Enter", 13, "\r"}, "Tab": {"Tab", 9, ""}, "Escape": {"Escape", 27, ""},
	"Backspace": {"Backspace", 8, ""}, "Delete": {"Delete", 46, ""}, "Space": {"Space", 32, " "},
	"ArrowUp": {"ArrowUp", 38, ""}, "ArrowDown": {"ArrowDown", 40, ""}, "ArrowLeft": {"ArrowLeft", 37, ""},
	"ArrowRight": {"ArrowRight", 39, ""}, "Home": {"Home", 36, ""}, "End": {"End", 35, ""},
	"PageUp": {"PageUp", 33, ""}, "PageDown": {"PageDown", 34, ""},
}

func (t *toolset) key(ctx context.Context, name string) error {
	k, ok := keyCodes[name]
	keyName := name
	if name == "Space" {
		keyName = " "
	}
	if !ok {
		if len([]rune(name)) != 1 {
			return fmt.Errorf("unsupported key %q", name)
		}
		_, err := t.b.page(ctx, "Input.insertText", map[string]any{"text": name})
		return err
	}
	down := map[string]any{"type": "keyDown", "key": keyName, "code": k.code, "windowsVirtualKeyCode": k.vk}
	if k.text != "" {
		down["text"] = k.text
	}
	if _, err := t.b.page(ctx, "Input.dispatchKeyEvent", down); err != nil {
		return err
	}
	_, err := t.b.page(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": keyName, "code": k.code, "windowsVirtualKeyCode": k.vk})
	return err
}

func (t *toolset) pressKey(ctx context.Context, a map[string]any) (string, error) {
	if err := t.key(ctx, sArg(a, "key")); err != nil {
		return "", err
	}
	time.Sleep(300 * time.Millisecond)
	t.b.waitReady(ctx, 10*time.Second)
	return "Pressed " + sArg(a, "key") + ".\n" + t.pageSummary(ctx), nil
}

func (t *toolset) back(ctx context.Context, _ map[string]any) (string, error) {
	if _, err := t.b.eval(ctx, "history.back()"); err != nil {
		return "", err
	}
	time.Sleep(500 * time.Millisecond)
	t.b.waitReady(ctx, 10*time.Second)
	return t.pageSummary(ctx), nil
}

func (t *toolset) waitFor(ctx context.Context, a map[string]any) (string, error) {
	secs := nArg(a, "seconds", 10)
	if secs > 30 {
		secs = 30
	}
	text := sArg(a, "text")
	deadline := time.Now().Add(time.Duration(secs * float64(time.Second)))
	if text == "" {
		time.Sleep(time.Until(deadline))
		return fmt.Sprintf("Waited %.0fs.", secs), nil
	}
	for time.Now().Before(deadline) {
		v, err := t.b.eval(ctx, "((document.body && document.body.innerText) || '').includes("+jsq(text)+")")
		if err == nil && string(v) == "true" {
			return "Found text.\n" + t.pageSummary(ctx), nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("text %q did not appear within %.0fs", text, secs)
}

func (t *toolset) evaluate(ctx context.Context, a map[string]any) (string, error) {
	v, err := t.b.eval(ctx, sArg(a, "expression"))
	if err != nil {
		return "", err
	}
	if len(v) == 0 {
		return "undefined", nil
	}
	return truncate(string(v), defaultMaxChars), nil
}

func (t *toolset) getHTML(ctx context.Context, a map[string]any) (string, error) {
	el := "document.documentElement"
	if sArg(a, "ref") != "" || sArg(a, "selector") != "" {
		var err error
		if el, err = elementJS(a); err != nil {
			return "", err
		}
	}
	v, err := t.b.eval(ctx, "(() => { const e = "+el+"; return e ? e.outerHTML : null; })()")
	if err != nil {
		return "", err
	}
	var s *string
	_ = json.Unmarshal(v, &s)
	if s == nil {
		return "", fmt.Errorf("element not found")
	}
	return truncate(*s, int(nArg(a, "max_chars", defaultMaxChars))), nil
}

func (t *toolset) cookies(ctx context.Context, _ map[string]any) (string, error) {
	raw, err := t.b.page(ctx, "Network.getCookies", nil)
	if err != nil {
		return "", err
	}
	var r struct {
		Cookies []struct {
			Name, Value, Domain, Path, SameSite string
			HTTPOnly                            bool `json:"httpOnly"`
			Secure                              bool
		}
	}
	_ = json.Unmarshal(raw, &r)
	if len(r.Cookies) == 0 {
		return "No cookies.", nil
	}
	var b strings.Builder
	for _, c := range r.Cookies {
		fmt.Fprintf(&b, "%s=%s; Domain=%s; Path=%s; HttpOnly=%v; Secure=%v; SameSite=%s\n",
			c.Name, c.Value, c.Domain, c.Path, c.HTTPOnly, c.Secure, c.SameSite)
	}
	return b.String(), nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (t *toolset) screenshot(ctx context.Context, a map[string]any) (string, error) {
	p := map[string]any{"format": "png"}
	if bArg(a, "full_page", false) {
		raw, err := t.b.page(ctx, "Page.getLayoutMetrics", nil)
		if err != nil {
			return "", err
		}
		var m struct {
			CSSContentSize struct{ Width, Height float64 } `json:"cssContentSize"`
		}
		_ = json.Unmarshal(raw, &m)
		if m.CSSContentSize.Width > 0 && m.CSSContentSize.Height > 0 {
			p["captureBeyondViewport"] = true
			p["clip"] = map[string]any{"x": 0, "y": 0, "width": m.CSSContentSize.Width,
				"height": min(m.CSSContentSize.Height, 16000), "scale": 1}
		}
	}
	raw, err := t.b.page(ctx, "Page.captureScreenshot", p)
	if err != nil {
		return "", err
	}
	var r struct{ Data string }
	_ = json.Unmarshal(raw, &r)
	png, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return "", err
	}
	name := strings.TrimLeft(filepath.Base(unsafeName.ReplaceAllString(sArg(a, "filename"), "_")), "._")
	if name == "" {
		name = "screenshot-" + time.Now().Format("20060102-150405.000")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".png") {
		name += ".png"
	}
	if err := os.MkdirAll(t.b.cfg.OutputDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(t.b.cfg.OutputDir, name)
	if err := os.WriteFile(path, png, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved screenshot (%d bytes) to %s", len(png), path), nil
}

func (t *toolset) closeBrowser(context.Context, map[string]any) (string, error) {
	t.b.Close()
	return "Browser closed.", nil
}
