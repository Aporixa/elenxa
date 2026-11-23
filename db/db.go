// Package db is the PostgreSQL data source for Elenxa (replacing the old single-file graph SQLite).
// It opens the connection, applies the schema, and seeds the built-in agents and variable catalog.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Aporixa/elenxa/config"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver ("pgx")
)

//go:embed schema.sql
var schemaSQL string

const schemaMigrationLockKey int64 = 7337741001

// maxOpenConns is the connection pool cap; see the explanation in Open. It is far below PostgreSQL's default
// max_connections=100, while far above the application's own nested connection-acquisition depth (the startup schema
// advisory lock holds one connection while seedBuiltins acquires another), so it cannot deadlock itself.
const maxOpenConns = 32

var schemaDeadlockRetryDelays = [...]time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
}

type schemaExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func isPostgresDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func applySchemaWithRetry(ctx context.Context, execer schemaExecer, sleep func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		if _, err := execer.ExecContext(ctx, schemaSQL); err != nil {
			if !isPostgresDeadlock(err) || attempt >= len(schemaDeadlockRetryDelays) {
				return err
			}
			sleep(schemaDeadlockRetryDelays[attempt])
			continue
		}
		return nil
	}
}

// withSchemaMigrationLock pins the session-level lock to one checked-out
// connection. Running pg_advisory_lock through *sql.DB is incorrect because a
// later schema or unlock call may use a different pooled PostgreSQL session.
func withSchemaMigrationLock(ctx context.Context, sqlDB *sql.DB, action func(*sql.Conn) error) (err error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationLockKey); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer func() {
		if _, unlockErr := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationLockKey); unlockErr != nil && err == nil {
			err = fmt.Errorf("advisory unlock: %w", unlockErr)
		}
	}()
	return action(conn)
}

// coordinateWithSchemaMigration makes long, multi-table archive transactions
// mutually exclusive with startup DDL while allowing ordinary runtime queries
// to continue normally.
func coordinateWithSchemaMigration(tx *sql.Tx) error {
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, schemaMigrationLockKey); err != nil {
		return fmt.Errorf("coordinate with schema migration: %w", err)
	}
	return nil
}

// DSN resolves the PostgreSQL connection string and reports where it came from.
// Precedence: env ELENXA_PG_DSN > config file (config.json). There is no
// built-in default — it errors if neither source is configured.
func DSN() (dsn, source string, err error) {
	return config.PostgresDSN()
}

// DB wraps the shared *sql.DB. PG handles its own connection pool + concurrency
// (MVCC), so unlike the old SQLite store there is no process-wide write mutex.
type DB struct{ *sql.DB }

// ensureDatabase connects to the postgres system database and creates the target
// database if it does not exist. dsn must be a postgres:// URL.
func ensureDatabase(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil // unparseable DSN — let the normal Open fail with a clear error
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}
	// connect to the postgres maintenance database instead
	adminDSN := *u
	adminDSN.Path = "/postgres"
	admin, err := sql.Open("pgx", adminDSN.String())
	if err != nil {
		return nil // best-effort; let Open surface the real error
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		return nil
	}
	var exists bool
	_ = admin.QueryRow(`SELECT true FROM pg_database WHERE datname=$1`, dbName).Scan(&exists)
	if !exists {
		if _, err := admin.Exec(`CREATE DATABASE "` + dbName + `"`); err != nil {
			return fmt.Errorf("create database %q: %w", dbName, err)
		}
	}
	return nil
}

// Open connects, applies the schema (idempotent), and seeds builtin rows.
func Open(dsn string) (*DB, error) {
	if err := ensureDatabase(dsn); err != nil {
		return nil, err
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// database/sql does not limit connections by default: when the pool has no idle connection it unconditionally opens a new one, all the way up
	// to PostgreSQL's max_connections (default 100) before being refused, so queries at peak get
	// an **error** like `FATAL: sorry, too many clients already`. With a cap, excess queries instead
	// queue for an idle connection — under the same load they get slower rather than fail, and callers no longer have to distinguish
	// "could not read" from "does not exist". maxOpenConns must leave headroom for psql / reset-password.sh and
	// any other instances that may run alongside; if max_connections has been lowered, lower this too.
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping postgres (%s): %w", config.Redact(dsn), err)
	}
	d := &DB{sqlDB}
	// pgx runs multi-statement Exec via the simple protocol when there are no args.
	// Keep the dedicated lock connection checked out until both DDL and seeding
	// finish so concurrent application instances cannot initialize out of order.
	err = withSchemaMigrationLock(context.Background(), sqlDB, func(conn *sql.Conn) error {
		if err := applySchemaWithRetry(context.Background(), conn, time.Sleep); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
		if err := d.seedBuiltins(); err != nil {
			return fmt.Errorf("seed builtins: %w", err)
		}
		return nil
	})
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

// browserMCPDefaultArgs is the default argument list of the bundled browser MCP.
const browserMCPDefaultArgs = `["--headless"]`

// builtinAgent describes one of the fixed agents and its prompt-variable catalog.
type builtinAgent struct {
	key, name, role, desc string
	vars                  []promptVar
	interactiveShell      bool // default interactive shell toggle when the row is created; ON CONFLICT does not override the user's later manual toggle
	runSeconds            *int // per-run wall-clock cap (seconds) when the row is created; nil = use the seed default (1200), 0 = no limit
}

type promptVar struct{ name, desc, example, source string }

// intp returns a pointer to v, used to set builtinAgent optional fields (such as runSeconds) explicitly.
func intp(v int) *int { return &v }

// builtinAgents mirrors docs §5(a). Built-in tools are not stored in the database; only agents + the variable catalog are seeded here.
// Interactive shell (persistent PTY tool family) ships on for the worker and main
// agent in this research branch. interactiveShell is only the value used when the
// row is first created — ON CONFLICT never overrides a later user toggle.
var builtinAgents = []builtinAgent{
	{"goals", "Goal breakdown", "goals", "Break the pentest task goal down into several independent, verifiable sub-goals.", []promptVar{
		{"EngagementDescription", "Task description (test target/background)", "Test the example.com site", "exploration"},
		// Now is a global runtime variable (see server.globalPromptVars) and is no longer defined again in each agent's catalog,
		// otherwise it would clash by name with the global entry when withGlobalVars appends it.
	}, false, nil},
	{"planner", "Planning", "planner", "Reads the situation, judges the goal, and only adds exploration intents when there is genuinely an uncovered new direction (one planning loop per task).", []promptVar{
		{"Goal", "Overall task goal", "Gain admin access to example.com", "exploration"},
		{"AssetSummary", "Asset count/type distribution summary (optional)", "domain:3 ip:5 site:2", "distilled"},
	}, false, nil},
	{"mainagent", "Main", "main", "Human-machine interface: observes progress and turns human intent into hints or high-priority intents.", []promptVar{
		{"Goal", "Current task goal", "Gain admin access to example.com", "exploration"},
		{"AssetSummary", "Opening situation summary (optional)", "domain:3 ip:5", "distilled"},
		{"FindingsSummary", "Confirmed vulnerability summary (optional)", "high:1 medium:2", "distilled"},
	}, true, nil},
	{"worker", "Execution", "worker", "Claims one intent, executes it, writes discovered facts/vulnerabilities back to the knowledge graph, then stops.", []promptVar{
		{"ProxyAddr", "Recording proxy address (drives the if two-variant text)", "127.0.0.1:8080", "runtime"},
		{"WorkerName", "Worker self-identifier (optional)", "worker-1", "runtime"},
	}, true, nil},
	// Auto: the built-in "platform operations" agent. It does not take part in the pentest orchestration loop; driven from the chat page, it uses tools to operate the platform.
	{"auto", "Auto", "assistant", "Platform operations assistant: uses tools to manage tasks (create/view/pause/give hints) and assets, and can create/modify skills, custom tools and MCP.", nil, false, nil},
	// Pentest: the built-in "standalone pentest" agent. Driven from the chat page, a single agent walks the whole pentest chain from recon to wrap-up, planning, executing and verifying on its own. Interactive shell is off by default (enable per agent).
	{"pentest", "Pentest", "assistant", "Standalone pentest agent: a single agent walks the whole chain from recon → finding attack surface → deep exploitation → verification → wrap-up, planning on its own, executing on its own, and verifying adversarially on its own.", nil, false, intp(0)},
}

// seedBuiltins inserts the fixed built-in agents and their variable catalog (idempotent).
func (d *DB) seedBuiltins() error {
	for _, a := range builtinAgents {
		var agentID int64
		err := d.QueryRow(`
INSERT INTO agents(key, name, description, role, builtin, enabled, interactive_shell, run_seconds)
VALUES ($1, $2, NULLIF($3,''), $4, true, true, $5, COALESCE($6, 1200))
ON CONFLICT (key) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
RETURNING id`, a.key, a.name, a.desc, a.role, a.interactiveShell, a.runSeconds).Scan(&agentID)
		if err != nil {
			return fmt.Errorf("agent %s: %w", a.key, err)
		}
		for _, v := range a.vars {
			if _, err := d.Exec(`
INSERT INTO agent_prompt_vars(agent_id, var_name, description, example, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (agent_id, var_name) DO UPDATE
  SET description = EXCLUDED.description, example = EXCLUDED.example, source = EXCLUDED.source`,
				agentID, v.name, v.desc, v.example, v.source); err != nil {
				return fmt.Errorf("agent %s var %s: %w", a.key, v.name, err)
			}
		}
	}
	// Drop catalog entries for variables that were renamed, so the white-list no
	// longer advertises a name templates can't resolve (EngagementTitle→Description).
	// After 'Now' was promoted from each agent's catalog to a global runtime variable, old databases still have a leftover 'Now' under goals
	// that clashes by name with the global entry (duplicate key in the frontend variable list); clean it up too.
	if _, err := d.Exec(`DELETE FROM agent_prompt_vars WHERE var_name IN ('EngagementTitle', 'CoverageGaps', 'Now')`); err != nil {
		return fmt.Errorf("cleanup renamed vars: %w", err)
	}
	// Existing rows keep their current value here; the research-branch one-time
	// migration in server enables the worker/main agent without repeatedly
	// overriding later operator changes.
	//
	// Seed the built-in browser MCP once — DISABLED by default, no proxy by default.
	// It is the bundled stdio binary /app/elenxa-browser-mcp (no npx / network fetch).
	// The traffic-capture toggle injects/strips --proxy-server / --proxy-ca at runtime
	// (server.Manager.syncBrowserMCPProxy). Insert only if absent so we never clobber
	// user edits (args/env/enabled/visibility) on restart.
	if _, err := d.Exec(`
INSERT INTO mcp_servers(name, transport, command, args, env, enabled)
VALUES ('browser', 'stdio', '/app/elenxa-browser-mcp', $1, '{}', false)
ON CONFLICT (name) DO NOTHING`,
		browserMCPDefaultArgs); err != nil {
		return fmt.Errorf("seed browser mcp: %w", err)
	}
	// Migrate a legacy browser row that still launches `npx @playwright/mcp` (which
	// fetches code from the npm registry at runtime) to the bundled binary. Idempotent:
	// only matches rows whose command is still npx.
	if _, err := d.Exec(`
UPDATE mcp_servers SET command = '/app/elenxa-browser-mcp', args = $1
WHERE name = 'browser' AND command = 'npx'`, browserMCPDefaultArgs); err != nil {
		return fmt.Errorf("migrate browser mcp: %w", err)
	}
	if err := d.seedBuiltinSkillVisibility(); err != nil {
		return fmt.Errorf("seed skill visibility: %w", err)
	}
	if err := d.seedDefaultInterceptRules(); err != nil {
		return fmt.Errorf("seed intercept rules: %w", err)
	}
	if err := d.seedDefaultInterceptRulesV2(); err != nil {
		return fmt.Errorf("seed intercept rules v2: %w", err)
	}
	if err := d.seedDefaultInterceptRulesV3(); err != nil {
		return fmt.Errorf("seed intercept rules v3: %w", err)
	}
	if err := d.seedDefaultAssetInterceptRules(); err != nil {
		return fmt.Errorf("seed asset intercept rules: %w", err)
	}
	return nil
}

// seedDefaultAssetInterceptRules inserts the built-in asset blocklist (fuzzy
// domain matches for government / education sites) once on first startup. Gated
// by a settings flag so a user's later disable/delete is never resurrected on
// restart — same policy as the intercept-rule seed.
func (d *DB) seedDefaultAssetInterceptRules() error {
	if v, _, _ := d.GetSetting("asset_intercept_default_rules_v1"); v == "done" {
		return nil
	}
	rules := []struct {
		kind    string
		pattern string
		note    string
	}{
		{"fuzzy_domain", ".gov", "[Built-in] Government sites (.gov)"},
		{"fuzzy_domain", ".gov.cn", "[Built-in] Government sites (.gov.cn)"},
		{"fuzzy_domain", ".edu", "[Built-in] Education sites (.edu)"},
		{"fuzzy_domain", ".edu.cn", "[Built-in] Education sites (.edu.cn)"},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO asset_intercept_rules(enabled, kind, pattern, note, builtin)
VALUES (true, $1, $2, $3, true)
ON CONFLICT DO NOTHING`, r.kind, r.pattern, r.note); err != nil {
			return fmt.Errorf("asset rule %q: %w", r.pattern, err)
		}
	}
	return d.SetSetting("asset_intercept_default_rules_v1", "done")
}

// builtinSkillVisibility maps a shipped skill's directory name → the built-in
// agent keys that should see it by default. The skill FILES themselves live on the
// filesystem (SkillDir, loaded by norma at runtime); DB only carries this visibility
// binding. Skills omitted here (e.g. playwright-cli) ship invisible by default —
// the user turns them on per-agent when needed.
var builtinSkillVisibility = map[string][]string{
	"api-recon":                   {"auto", "pentest", "worker"},
	"adversarial-environment":     {"auto", "pentest", "worker"},
	"ai-target-research":          {"auto", "pentest", "worker"},
	"lab-operations":              {"auto", "pentest", "worker"},
	"web-surface-mapping":         {"auto", "pentest", "worker"},
	"auth-session-testing":        {"auto", "pentest", "worker"},
	"authorization-testing":       {"auto", "pentest", "worker"},
	"injection-testing":           {"auto", "pentest", "worker"},
	"file-path-testing":           {"auto", "pentest", "worker"},
	"api-protocol-testing":        {"auto", "pentest", "worker"},
	"business-logic-testing":      {"auto", "pentest", "worker"},
	"client-browser-testing":      {"auto", "pentest", "worker"},
	"network-service-enumeration": {"auto", "pentest", "worker"},
	"evidence-verification":       {"auto", "pentest", "worker", "mainagent"},
}

// seedBuiltinSkillVisibility binds the shipped built-in skills to their default
// agents. Insert-if-absent (ON CONFLICT DO NOTHING) so a user's later toggle-off is
// never resurrected on restart — matches the browser-MCP / intercept-rule seed policy.
func (d *DB) seedBuiltinSkillVisibility() error {
	for skillName, agentKeys := range builtinSkillVisibility {
		for _, key := range agentKeys {
			if _, err := d.Exec(`
INSERT INTO agent_skill_visibility(agent_id, skill_name, enabled)
SELECT id, $2, true FROM agents WHERE key=$1
ON CONFLICT (agent_id, skill_name) DO NOTHING`, key, skillName); err != nil {
				return fmt.Errorf("skill %s → agent %s: %w", skillName, key, err)
			}
		}
	}
	return nil
}

// seedDefaultInterceptRules inserts built-in safety intercept rules once on
// first startup. The seed is gated by a settings flag so user edits (disable,
// delete, re-order) are never overwritten on subsequent restarts.
func (d *DB) seedDefaultInterceptRules() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v1"); v == "done" {
		return nil
	}
	type rule struct {
		name     string
		target   string // tool_name | tool_input
		typ      string // string | regex
		pattern  string
		action   string
		message  string
		priority int
	}
	rules := []rule{
		// ── Destructive system commands (priority 100) ──────────────────────────────────
		{
			name:     "[Built-in] Recursive force delete rm -rf",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\brm\b.{0,80}(?:-[a-z]*r[a-z]*f[a-z]*|-[a-z]*f[a-z]*r[a-z]*|--recursive|--no-preserve-root)`,
			action:   "deny",
			message:  "Recursive force delete (rm -rf / rm --recursive) is forbidden; it may permanently damage the system or target environment",
			priority: 100,
		},
		{
			name:     "[Built-in] Delete critical system directories",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\brm\b[^"'\n]{0,60}["'\s](/|/etc|/bin|/usr|/boot|/var|/lib|/sys|/proc|/dev|/sbin|/root)`,
			action:   "deny",
			message:  "Deleting critical system paths is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Disk format mkfs",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bmkfs\b`,
			action:   "deny",
			message:  "Formatting disks (mkfs) is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Overwrite disk device dd",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bdd\b[^|\n]{0,100}\bof=\s*/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Using dd to overwrite disk devices is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Fork bomb",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `:\(\)\s*\{[^}]*:\|:`,
			action:   "deny",
			message:  "Running a fork bomb is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Shutdown / reboot",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shutdown|reboot|halt|poweroff|init\s+[06])\b`,
			action:   "deny",
			message:  "Running shutdown or reboot commands is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Kill all processes",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\bkill\s+-9\s+-1\b|\bkillall\s+-9\b`,
			action:   "deny",
			message:  "kill -9 -1 or killall -9 (killing all processes) is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Disk wipe shred / wipe",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\b(?:shred|wipe)\b[^|\n]{0,80}/dev/[a-zA-Z]`,
			action:   "deny",
			message:  "Running shred/wipe on disk devices is forbidden",
			priority: 100,
		},
		{
			name:     "[Built-in] Flush firewall rules",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `\biptables\s+(?:-F|--flush)\b|\bnft\s+flush\s+ruleset\b`,
			action:   "deny",
			message:  "Flushing firewall rules (iptables -F / nft flush) is forbidden",
			priority: 100,
		},
		// ── Destructive database operations (priority 90) ─────────────────────────────────
		{
			name:     "[Built-in] SQL DROP DATABASE / TABLE / SCHEMA",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bDROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`,
			action:   "deny",
			message:  "DROP operations are forbidden; they may irreversibly destroy database objects",
			priority: 90,
		},
		{
			name:     "[Built-in] SQL TRUNCATE",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bTRUNCATE\s+(?:TABLE\s+)?\w`,
			action:   "deny",
			message:  "TRUNCATE is forbidden; it may wipe all data in a table",
			priority: 90,
		},
		{
			name:     "[Built-in] MongoDB drop / dropDatabase",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\.(?:dropDatabase|dropCollection|drop)\s*\(`,
			action:   "deny",
			message:  "MongoDB drop operations are forbidden",
			priority: 90,
		},
		{
			name:     "[Built-in] Redis FLUSHALL / FLUSHDB",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\b(?:FLUSHALL|FLUSHDB)\b`,
			action:   "deny",
			message:  "Redis FLUSHALL / FLUSHDB is forbidden; it may wipe all cached data",
			priority: 90,
		},
		// ── Destructive HTTP requests (priority 80) ──────────────────────────────────
		// Three common ways an agent sends a DELETE request:
		//   1. curl -X DELETE / --request DELETE (run directly by the Bash tool or written into a script)
		//   2. The .delete() method of Python HTTP clients
		//   3. method: 'DELETE' / method="DELETE" in JS/generic scripts
		{
			name:     "[Built-in] curl / wget sends DELETE request",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\bcurl\b[^|\n&;"]{0,300}(?:-X\s*DELETE|--request\s+DELETE|-XDELETE)|\bwget\b[^|\n&;"]{0,300}--method[=\s]+DELETE`,
			action:   "deny",
			message:  "Sending HTTP DELETE requests via curl/wget is forbidden; it may delete target system data",
			priority: 80,
		},
		{
			name:     "[Built-in] Python HTTP client DELETE (requests/httpx/aiohttp)",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)\b(?:requests|httpx|aiohttp|urllib\.request)\.delete\s*\(|session\.delete\s*\(|client\.delete\s*\(`,
			action:   "deny",
			message:  "Sending DELETE requests with Python HTTP clients is forbidden",
			priority: 80,
		},
		{
			name:     "[Built-in] HTTP DELETE method declared in script (JS/generic)",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)axios\.delete\s*\(|method\s*[:=]\s*['"]DELETE['"]`,
			action:   "deny",
			message:  "Declaring and sending HTTP DELETE requests in scripts is forbidden",
			priority: 80,
		},
		{
			name:     "[Built-in] Bulk clear / purge endpoint paths",
			target:   "tool_input",
			typ:      "regex",
			pattern:  `(?i)/(?:clear|wipe|flush|purge|truncate|drop|destroy|factory[-_]reset|reset[-_]all)(?:[/?#"'\s]|$)`,
			action:   "deny",
			message:  "Calling bulk clear or destroy endpoints (/clear /wipe /flush /purge etc.) is forbidden",
			priority: 80,
		},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
VALUES ($1, true, $2, $3, $4, $5, $6, $7, false, 60, 'deny')
ON CONFLICT DO NOTHING`,
			r.name, r.priority, r.target, r.typ, r.pattern, r.action, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_default_rules_v1", "done")
}

// seedDefaultInterceptRulesV2 migrates the two safety patterns that used to be
// hard-coded in guard.go (destructive shell + data-exfil pipe) into ordinary
// intercept rules. Gated by its own flag so it also lands on DBs that already ran
// v1. Unlike the old guard.go floor, these are plain [Built-in] rules — the user can
// disable or delete them. The exfil rule ships ENABLED (hardened default); it can
// mis-fire on legitimate reverse-shell / data-transfer pipes, in which case the
// user can disable it explicitly.
func (d *DB) seedDefaultInterceptRulesV2() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v2"); v == "done" {
		return nil
	}
	rules := []struct {
		name     string
		pattern  string
		action   string
		message  string
		enabled  bool
		priority int
	}{
		{
			name:     "[Built-in] Destructive system commands",
			pattern:  `(?i)\b(rm\s+-rf\s+/|mkfs|dd\s+if=|:\(\)\s*\{|shutdown|reboot|>\s*/dev/sd)`,
			action:   "deny",
			message:  "Destructive command denied (rm -rf / / mkfs / dd / fork bomb / shutdown/reboot / overwrite disk device)",
			enabled:  true,
			priority: 100,
		},
		{
			name:     "[Built-in] Data exfiltration pipe",
			pattern:  `(?i)(curl|wget|nc|ncat)\b[^|]*\b(\|\s*(curl|wget|nc))`,
			action:   "deny",
			message:  "Suspected data exfiltration pipe denied (command output sent out via curl/wget/nc)",
			enabled:  true,
			priority: 80,
		},
	}
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
VALUES ($1, $2, $3, 'tool_input', 'regex', $4, $5, $6, false, 60, 'deny')
ON CONFLICT DO NOTHING`,
			r.name, r.enabled, r.priority, r.pattern, r.action, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_default_rules_v2", "done")
}

// seedDefaultInterceptRulesV3 adds the delete-endpoint path rule. The v1 HTTP rules
// only catch the DELETE *method* (curl -X DELETE, requests.delete(, method:'DELETE'),
// and v1's path rule covers only /clear /wipe /flush /purge /truncate /drop /destroy
// /factory-reset /reset-all — so a plain `curl 'http://t/api/user/delete?id=1'` (a
// delete endpoint reached with GET/POST, which is how most web apps expose deletion)
// slipped through every built-in rule. Own flag so it also lands on DBs that already
// ran v1/v2, where editing the v1 seed would have no effect.
//
// The pattern deliberately requires a separator after the verb so /delivery,
// /details, /delta and /delegate do not match, while /deleteAll, /delete_user and
// /delete-user do. destroy is re-covered here because v1's rule does not allow a
// suffix (/destroyAll was missed).
//
// Exported as a package const only so the seeded regex is unit-testable without a DB.
const deleteEndpointPathPattern = `(?i)/(?:(?:delete|remove|unlink|erase|destroy)[-\w]*|del)(?:[/?#"'\s]|$)`

func (d *DB) seedDefaultInterceptRulesV3() error {
	if v, _, _ := d.GetSetting("intercept_default_rules_v3"); v == "done" {
		return nil
	}
	const name = "[Built-in] Delete endpoint paths"
	if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
SELECT $1, true, 80, 'tool_input', 'regex', $2, 'deny', $3, false, 60, 'deny'
WHERE NOT EXISTS (SELECT 1 FROM intercept_rules WHERE name = $1)`,
		name,
		deleteEndpointPathPattern,
		"Calling delete endpoints (/delete /remove /unlink /erase etc.) is forbidden regardless of HTTP method — most applications' delete endpoints can be triggered with GET/POST and will really delete target data",
	); err != nil {
		return fmt.Errorf("rule %q: %w", name, err)
	}
	return d.SetSetting("intercept_default_rules_v3", "done")
}
