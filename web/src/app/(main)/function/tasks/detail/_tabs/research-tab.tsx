"use client";

import * as React from "react";

import {
  DownloadIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
  TargetIcon,
  TriangleAlertIcon,
} from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import type { ResearchCall, ResearchMetricsReport } from "@/lib/types";

function count(values: Record<string, number> | undefined, key: string): number {
  return values?.[key] ?? 0;
}

function pct(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function label(value: string): string {
  return value.replaceAll("_", " ");
}

function outcomeVariant(outcome: ResearchCall["outcome"]): "default" | "secondary" | "destructive" | "outline" {
  if (outcome === "completed") return "default";
  if (outcome === "permission_denied" || outcome === "hook_blocked") return "secondary";
  if (outcome === "error") return "destructive";
  return "outline";
}

function download(report: ResearchMetricsReport, taskId: string) {
  const blob = new Blob([JSON.stringify(report, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `elenxa-task-${taskId}-research-metrics.json`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function ResearchTab({ taskId }: { taskId: string }) {
  const [report, setReport] = React.useState<ResearchMetricsReport | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");

  const load = React.useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setReport(await api.researchMetrics(taskId));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load research metrics");
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  React.useEffect(() => {
    void load();
  }, [load]);

  if (error) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>Research metrics unavailable</AlertTitle>
        <AlertDescription className="flex items-center justify-between gap-3">
          <span>{error}</span>
          <Button size="sm" variant="outline" onClick={() => void load()}>
            Retry
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  if (!report) {
    return (
      <p className="text-sm text-muted-foreground">{loading ? "Loading research metrics…" : "No metrics available."}</p>
    );
  }

  const { summary } = report;
  const resolved = count(summary.scope, "in_scope") + count(summary.scope, "out_of_scope");
  const blocked = count(summary.outcomes, "permission_denied") + count(summary.outcomes, "hook_blocked");

  const cards = [
    {
      title: "Attempted calls",
      value: summary.total_attempts,
      detail: `${count(summary.outcomes, "completed")} completed`,
      icon: TargetIcon,
    },
    {
      title: "Scope deviation",
      value: pct(summary.deviation_rate),
      detail: `${count(summary.scope, "out_of_scope")} of ${resolved} resolved network calls`,
      icon: ShieldAlertIcon,
    },
    {
      title: "Scope resolution",
      value: pct(summary.resolution_rate),
      detail: `${count(summary.scope, "unresolved")} unresolved calls`,
      icon: ShieldCheckIcon,
    },
    {
      title: "Control blocks",
      value: blocked,
      detail: `${count(summary.outcomes, "permission_denied")} permission · ${count(summary.outcomes, "hook_blocked")} hook`,
      icon: TriangleAlertIcon,
    },
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">Research behavior</h2>
          <p className="text-sm text-muted-foreground">
            Model-proposed calls are measured separately from execution outcomes and target effects.
          </p>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => void load()} disabled={loading}>
            <RefreshCwIcon className={loading ? "animate-spin" : ""} /> Refresh
          </Button>
          <Button size="sm" variant="outline" onClick={() => download(report, taskId)}>
            <DownloadIcon /> Export JSON
          </Button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {cards.map(({ title, value, detail, icon: Icon }) => (
          <Card key={title} className="gap-2">
            <CardHeader className="pb-0">
              <CardDescription className="flex items-center gap-1.5">
                <Icon className="size-3.5" /> {title}
              </CardDescription>
              <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground">{detail}</CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Benchmark configuration</CardTitle>
          <CardDescription>
            Sanitized task, model, skill, shell, constraint, and control settings captured with this report.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-3">
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Models</p>
            {report.context.models.map((model) => (
              <p key={model.role} className="mt-1 text-sm">
                {model.role}: <span className="font-mono text-xs">{model.model || model.reason || "unresolved"}</span>
              </p>
            ))}
          </div>
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Agent capabilities</p>
            {report.context.agents.map((agent) => (
              <p key={agent.key} className="mt-1 text-sm">
                {agent.key}: {agent.interactive_shell ? "persistent shell" : "no persistent shell"} ·{" "}
                {agent.skills.length} skills
              </p>
            ))}
          </div>
          <div>
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Configured controls</p>
            <p className="mt-1 text-sm">{report.context.constraints.length} task constraints</p>
            <p className="mt-1 text-sm">{report.context.controls.tool_rules.length} tool rules</p>
            <p className="mt-1 text-sm">
              {report.context.controls.global_asset_rules.length + report.context.controls.task_asset_rules.length}{" "}
              asset rules
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Adversarial events and oracle checks</CardTitle>
          <CardDescription>
            Structured observations recorded by the research skills and preserved in the task graph.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {(report.events ?? []).map((event) => (
            <div key={event.activity_id} className="rounded-md border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={event.recorded ? "outline" : "destructive"}>{label(event.event_type)}</Badge>
                {event.changed_plan && <Badge variant="secondary">changed plan</Badge>}
                {event.impact && <span className="text-xs text-muted-foreground">impact: {label(event.impact)}</span>}
              </div>
              <p className="mt-2 text-sm font-medium">{event.summary}</p>
              {event.artifact && <p className="mt-1 text-xs text-muted-foreground">Artifact: {event.artifact}</p>}
              {event.corroboration && (
                <p className="mt-1 text-xs text-muted-foreground">Corroboration: {event.corroboration}</p>
              )}
              {event.recovery && <p className="mt-1 text-xs text-muted-foreground">Recovery: {event.recovery}</p>}
            </div>
          ))}
          {(report.events ?? []).length === 0 && (
            <p className="py-4 text-center text-sm text-muted-foreground">
              No structured research events recorded yet.
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Attempt ledger</CardTitle>
          <CardDescription>
            Literal targets are classified against the task scope snapshot. Dynamic targets remain unresolved.
          </CardDescription>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>Agent</TableHead>
                <TableHead>Tool</TableHead>
                <TableHead>Outcome</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Explicit targets</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {report.calls.map((call) => (
                <TableRow key={call.activity_id}>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {new Date(call.attempted_at).toLocaleString()}
                  </TableCell>
                  <TableCell>{call.worker || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{call.tool}</TableCell>
                  <TableCell>
                    <Badge variant={outcomeVariant(call.outcome)}>{label(call.outcome)}</Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant={call.scope_classification === "out_of_scope" ? "destructive" : "outline"}>
                      {label(call.scope_classification)}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-72 text-xs">
                    {call.candidates.map((candidate) => candidate.host).join(", ") || "—"}
                  </TableCell>
                </TableRow>
              ))}
              {report.calls.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    No tool attempts recorded yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Alert>
        <TriangleAlertIcon />
        <AlertTitle>Interpretation limits</AlertTitle>
        <AlertDescription>
          <ul className="list-disc space-y-1 pl-5">
            {report.limits.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </AlertDescription>
      </Alert>
    </div>
  );
}
