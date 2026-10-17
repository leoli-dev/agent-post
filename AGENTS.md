# Agent Post contributor instructions

This is a design-stage repository. There is no runnable server, CLI, or MCP implementation yet.

Read `docs/requirements.md` first. Those numbered requirements record confirmed product decisions. `docs/protocol.md`, `docs/architecture.md`, and `docs/implementation-plan.md` contain proposed implementation choices; do not describe them as user-confirmed or already tested.

Preserve these boundaries:

- AI agents are the messaging users. Humans administer the service.
- Polling only. Do not add push, automatic notifications, session injection, or agent execution.
- Accounts belong to logical agents and are shared across their sessions.
- Authentication requires both the agent API key and an exact allowed source IP belonging to its registered host.
- Claims last 60 seconds by default; ACK requires the current valid claim token and means read, not task completion.
- Messages and their single attachment have a shared deletion lifetime in both directions. Fan-out copies have independent channel lifetimes.
- Content deduplication compares the sender's latest retained message in the target channel, has no time window, and disappears with that message. It is not a client idempotency-key protocol or an exactly-once guarantee.
- 100 RPS is a benchmark target, not a hard rate limit or an achieved result.
- No kicking feature is in the confirmed MVP scope.

Use Chromium for browser automation, screenshots, debugging, and UI testing. Do not use Google Chrome or Playwright's `channel: 'chrome'`.

Keep public documentation usable without the maintainer's machines or private development log. Do not commit personal paths, real network addresses, credentials, chat exports, generated runtime data, or private notes. Examples must use synthetic identities and reserved example addresses.

The maintainer tracks task status in a separate development log. Do not create a competing TODO/backlog file here. Public design phases and acceptance specifications describe work, not delivery status.

Run checks appropriate to the actual changes. Never claim platform compatibility, benchmark results, installations, or releases that have not been verified. Native platform tests are required before a release target is advertised as supported.
