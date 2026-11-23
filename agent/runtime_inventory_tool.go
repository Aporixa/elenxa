package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"sort"
	"strings"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// runtimeCommands is deliberately explicit: the inventory reports the curated
// lab runtime plus common optional tools a custom image may add. It never scans
// arbitrary directories and never installs or executes anything.
var runtimeCommands = map[string][]string{
	"core":     {"bash", "curl", "file", "git", "jq", "node", "openssl", "python3", "rg", "unzip", "wget"},
	"network":  {"dig", "host", "nc", "nmap", "ping", "socat", "telnet", "whois"},
	"web":      {"chromium", "ffuf", "sqlmap", "whatweb", "xmlstarlet"},
	"optional": {"gobuster", "hashcat", "hydra", "john", "nikto", "nuclei", "wfuzz"},
}

// runtimeInventoryPrompt gives every worker the actual shell inventory without
// spending a model tool call. The callable tool remains useful for a focused
// category refresh, but this code-owned prompt block prevents the model from
// assuming that commands from a familiar pentest image exist here.
func runtimeInventoryPrompt() string {
	var names []string
	for _, group := range runtimeCommands {
		names = append(names, group...)
	}
	sort.Strings(names)
	found := make([]string, 0, len(names))
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			found = append(found, name)
		} else {
			missing = append(missing, name)
		}
	}
	return "\n\n**Shell runtime inventory (verified for this container)**:\n- Installed: " + strings.Join(found, ", ") +
		"\n- Not installed: " + strings.Join(missing, ", ") +
		"\nUse only installed commands. Do not invent JSON wrapper tools such as `parallel`; emit independent normal tool calls together when concurrency is useful."
}

func runtimeInventoryTool() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "runtime_tool_inventory",
		Description: "Report which curated shell/security executables are actually installed in this Elenxa runtime. Call this before relying on an unfamiliar command. A missing executable is not a callable JSON tool; choose an installed alternative or report the limitation. Multiple normal tool calls can be emitted together without a `parallel` wrapper.",
		Schema: obj(map[string]any{
			"category": map[string]any{"type": "string", "enum": []string{"all", "core", "network", "web", "optional"}, "description": "Inventory category (default all)"},
		}),
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(_ context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var args struct {
				Category string `json:"category"`
			}
			_ = json.Unmarshal(input, &args)
			if args.Category == "" {
				args.Category = "all"
			}
			var names []string
			if args.Category == "all" {
				for _, group := range runtimeCommands {
					names = append(names, group...)
				}
			} else {
				var ok bool
				names, ok = runtimeCommands[args.Category]
				if !ok {
					return actool.Errorf("unknown inventory category"), nil
				}
			}
			sort.Strings(names)
			found := map[string]string{}
			missing := []string{}
			for _, name := range names {
				if path, err := exec.LookPath(name); err == nil {
					found[name] = path
				} else {
					missing = append(missing, name)
				}
			}
			return jsonResult(map[string]any{"category": args.Category, "found": found, "missing": missing})
		},
	})
}
