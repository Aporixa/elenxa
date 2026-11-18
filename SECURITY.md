# Security and responsible use

This is an autonomous offensive-security agent. It plans and runs real attacks:
scanning, fuzzing, exploitation and credential testing. Treat it like any other
attack tool.

## Authorized targets only

Point it only at systems you own, or that you hold **explicit, written
permission** to test. The permission must cover the specific scope (hosts, URLs,
IP ranges), the dates, and the kinds of testing.

- **Your own lab**: intentionally vulnerable apps (for example OWASP Juice Shop),
  your own VMs and containers, isolated networks you control.
- **Your own systems**: only where you are the owner or the authorized operator.
  Shared hosting, third-party SaaS and cloud provider infrastructure usually need
  the provider's permission as well, even when the account is yours.
- **Client systems**: only under a signed engagement with a defined scope and
  rules of engagement.

Do not use it against systems you do not own or are not authorized to test.
Unauthorized testing is illegal in most countries (for example the US Computer
Fraud and Abuse Act and the UK Computer Misuse Act), whatever the intent.

The agent decides its own next steps. It can wander beyond a loose target
description, follow links to other hosts, or run heavier attacks than you
expected. You are responsible for everything it does.

## Recommended operating setup

- Run it in an isolated VM or network with outbound traffic limited to the model
  provider's API and the in-scope targets. The research setup this build was made
  for uses a libvirt nwfilter that allows only the Anthropic API range and the lab.
- Keep the web UI bound to `127.0.0.1` (the default) and reach it over an SSH
  tunnel. Never expose it to the internet: it runs commands.
- Write the target scope into every task. Keep the guard hooks and approval
  settings on.
- Turn on LLM recording and traffic capture when you need an audit trail, and
  store those records as sensitive. They contain target data and full prompts.
- Keep API keys in `.env`, which git ignores. Never commit them.
- Do not test against production data that belongs to real people.

## Reporting a vulnerability in this project

If you find a security issue in this software itself, such as an authentication
bypass, sandbox or path-confinement escape, unwanted egress, or secret exposure,
please report it privately through GitHub's **Security → Report a
vulnerability** on this repository. Do not open a public issue.

Please include the version or commit, steps to reproduce, and the impact. We aim
to acknowledge within 7 days and agree on a disclosure date with you.

Findings about *targets* the tool tested are out of scope here. Report those to
the target's owner.

## No warranty

Provided as is, without warranty of any kind (see `LICENSE`, AGPL-3.0). The
authors and contributors are not liable for misuse or for any damage the tool
causes.
