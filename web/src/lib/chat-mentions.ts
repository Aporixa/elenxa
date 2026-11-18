// Wire token format: `@[<name>#<id> ...]`, stored in messages and parsed by server/chat_mentions.go.
// New tokens use the English `name`; `label` is the legacy Chinese form, still accepted (and typable
// as a search alias) so drafts and history from before the translation keep working.
export const mentionKinds = [
  { kind: "finding", label: "漏洞", name: "Finding", alias: "finding" },
  { kind: "asset", label: "资产", name: "Asset", alias: "asset" },
  { kind: "company", label: "企业", name: "Company", alias: "company" },
  { kind: "endpoint", label: "接口", name: "Endpoint", alias: "api" },
  { kind: "ip", label: "IP", name: "IP", alias: "ip" },
  { kind: "app", label: "应用", name: "App", alias: "app" },
  { kind: "root_domain", label: "域名", name: "Domain", alias: "domain" },
  { kind: "subdomain", label: "子域名", name: "Subdomain", alias: "subdomain" },
  { kind: "service", label: "服务", name: "Service", alias: "service" },
] as const;

export type MentionKind = (typeof mentionKinds)[number]["kind"];
export interface ChatMention {
  kind: MentionKind;
  id: number;
  label: string;
  description: string;
}

export function activeMention(value: string, caret: number) {
  const before = value.slice(0, caret);
  const start = before.lastIndexOf("@");
  if (start < 0 || (start > 0 && /[\w.+/-]/.test(before[start - 1]))) return null;
  const query = before.slice(start + 1);
  if (/[[\]\r\n@]/.test(query) || query.length > 220) return null;
  return { start, end: caret, query };
}

export function mentionSearch(query: string) {
  const text = query.trimStart().toLowerCase();
  for (const item of mentionKinds) {
    for (const alias of [item.label.toLowerCase(), item.alias, item.name.toLowerCase()]) {
      if (text === alias || text.startsWith(`${alias} `) || (/[^a-z]/.test(alias) && text.startsWith(alias))) {
        return { kind: item.kind, query: query.trimStart().slice(alias.length).trim(), categories: [] };
      }
    }
  }
  const categories = mentionKinds.filter(
    (item) =>
      item.label.toLowerCase().startsWith(text) ||
      item.alias.startsWith(text) ||
      item.name.toLowerCase().startsWith(text),
  );
  return { kind: "" as const, query: query.trim(), categories };
}

export function mentionToken(item: ChatMention) {
  const kind = mentionKinds.find((entry) => entry.kind === item.kind)?.name ?? "Asset";
  const label = item.label
    .replace(/[[\]]/g, (char) => (char === "[" ? "(" : ")"))
    .replace(/\s+/g, " ")
    .slice(0, 100);
  return `@[${kind}#${item.id} ${label}]`;
}

export function selectedMentions(value: string) {
  return [...value.matchAll(/@\[(漏洞|资产|企业|接口|IP|应用|域名|子域名|服务|Finding|Asset|Company|Endpoint|App|Domain|Subdomain|Service)#([0-9]+)(?: ([^\]\r\n]*))?\]/g)].map(
    (match) => ({
      token: match[0],
      label: `${mentionKinds.find((entry) => entry.label === match[1] || entry.name === match[1])?.name ?? match[1]} #${match[2]}${match[3] ? ` · ${match[3]}` : ""}`,
      start: match.index,
    }),
  );
}
