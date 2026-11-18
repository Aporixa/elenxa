// Command elenxa-browser-mcp is Elenxa's own browser MCP server.
//
// It replaces the previously seeded `npx @playwright/mcp`, which fetched code
// from npm at runtime. This server is stdlib-only Go, speaks MCP (JSON-RPC 2.0,
// newline-delimited) on stdin/stdout, and drives the system Chromium through the
// DevTools protocol over a private pipe (--remote-debugging-pipe), so no
// debugging port is ever opened.
//
// Flags (managed by Elenxa's traffic-capture toggle, see server.syncBrowserMCPProxy):
//
//	--headless            run Chromium headless (default true)
//	--proxy-server ADDR   route all browser traffic through Elenxa's recording proxy
//	--proxy-ca PATH       PEM of the recording proxy's MITM CA (see browser.go)
//	--chromium PATH       Chromium binary (default: first of chromium, chromium-browser, google-chrome on PATH)
//	--output-dir DIR      where screenshots are written (default $TMPDIR/elenxa-browser)
//	--no-sandbox          disable Chromium's sandbox (auto-enabled inside containers)
//	--allow-vendor-hosts  let the browser resolve google.com, gstatic.com, ... (blocked by
//	                      default so Chromium cannot phone home; see packaging/chromium-policy.json)
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
)

const serverName = "elenxa-browser"
const serverVersion = "1.0.0"

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcOut struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

func main() {
	var cfg browserConfig
	flag.BoolVar(&cfg.Headless, "headless", true, "run Chromium headless")
	flag.StringVar(&cfg.ProxyServer, "proxy-server", "", "proxy address for all browser traffic")
	flag.StringVar(&cfg.ProxyCA, "proxy-ca", "", "PEM path of the recording proxy's CA")
	flag.StringVar(&cfg.Chromium, "chromium", "", "Chromium binary path")
	flag.StringVar(&cfg.OutputDir, "output-dir", filepath.Join(os.TempDir(), "elenxa-browser"), "screenshot directory")
	flag.BoolVar(&cfg.NoSandbox, "no-sandbox", inContainer(), "disable Chromium sandbox")
	flag.BoolVar(&cfg.AllowVendorHosts, "allow-vendor-hosts", false, "let the browser resolve Google service domains (blocked by default)")
	flag.Parse()

	log.SetOutput(os.Stderr) // stdout is the MCP channel
	log.SetPrefix("[elenxa-browser-mcp] ")

	b := newBrowser(cfg)
	// Elenxa stops MCP servers by closing stdin or signalling; either way shut
	// Chromium down and remove its profile before exiting.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-sig
		b.Close()
		os.Exit(0)
	}()
	serve(os.Stdin, os.Stdout, newToolset(b))
	b.Close()
}

func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func serve(in io.Reader, out io.Writer, ts *toolset) {
	var wmu sync.Mutex
	enc := json.NewEncoder(out)
	write := func(v rpcOut) {
		wmu.Lock()
		defer wmu.Unlock()
		v.JSONRPC = "2.0"
		if err := enc.Encode(v); err != nil {
			log.Printf("write: %v", err)
		}
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m rpcMsg
		if err := json.Unmarshal(line, &m); err != nil {
			write(rpcOut{ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
			continue
		}
		if len(m.ID) == 0 || string(m.ID) == "null" {
			continue // notification (e.g. notifications/initialized): nothing to answer
		}
		// Tool calls are handled one at a time: there is a single browser page,
		// and concurrent actions on it would interleave unpredictably.
		res, err := handle(ts, m)
		if err != nil {
			write(rpcOut{ID: m.ID, Error: err})
			continue
		}
		write(rpcOut{ID: m.ID, Result: res})
	}
	if err := sc.Err(); err != nil {
		log.Printf("stdin: %v", err)
	}
}

func handle(ts *toolset, m rpcMsg) (any, *rpcErr) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &p)
		pv := p.ProtocolVersion
		if pv == "" {
			pv = "2024-11-05"
		}
		return map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": ts.list()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcErr{-32602, "invalid params"}
		}
		text, isErr := ts.call(context.Background(), p.Name, p.Arguments)
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": isErr,
		}, nil
	default:
		return nil, &rpcErr{-32601, fmt.Sprintf("method not found: %s", m.Method)}
	}
}
