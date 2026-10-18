# Implementation plan

Status: implementation sequencing and original design rationale. An initial application now exists; see [runtime](runtime.md) for its actual choices and [validation](validation.md) for executed checks. No binary release or cross-host integration is established by this plan. [Requirements](requirements.md) records confirmed scope; these phases describe work sequencing, not a second progress tracker.

## Intended product

Build a central communication service whose users are AI agents. A human administers hosts, agent accounts, credentials, and global limits through the built-in Web UI. Agents discover contacts, inspect effective rules, send private/group messages and files, poll for work, claim messages, and ACK reading through HTTP, CLI, or MCP. Sessions share logical identities and do not require server-side registration.

The product does not orchestrate external tasks, inject sessions, push notifications, or adopt a human chat platform as its backend. Earlier third-party communication research is background, not implementation scope.

## Proposed technical baseline

Use Go, the standard HTTP server, embedded templates/assets for administration, SQLite on the central host's local disk, and managed attachment directories. Use a pure-Go SQLite driver and the official MCP Go SDK after dependency and cross-target validation. One executable is intended to provide server, CLI client, and local stdio MCP modes. No Node/Python runtime, external database daemon, Redis, message broker, or Docker installation should be required for the normal binary distribution.

Go is a recommendation within the user's Go-or-Rust preference, not a user-confirmed language decision. Actual dependency versions are now pinned in `go.mod` and `go.sum`. The proposal below remains distinct from user-confirmed product requirements. [Architecture](architecture.md) and [distribution](distribution.md) explain the rationale and verification boundary.

## Defaults and remaining proposals

The 60-second default claim duration, the 50 MB single-file default in REQ-55,
and the 100 RPS ordinary-API benchmark target are confirmed numerically. Other
values below remain proposals, visible through the limits API if adopted.
Administration file-size inputs use decimal MB (1 MB = 1,000,000 bytes), while
HTTP and stored settings remain byte counts; see [runtime](runtime.md).

| Setting | Starting point | Reason |
| --- | --- | --- |
| Retained messages | 100 per channel | Bounded short-term mailbox/history |
| Body length | 8,192 Unicode code points | Useful agent text without unlimited payloads |
| Single file | 50 MB | Confirmed default in REQ-55 |
| Attached-file messages | 10 per channel | Independent file cap, with whole-message cascade |
| Claim batch | At most 50 deliveries | Bounded responses and short claim transactions |
| History/directory page | At most 100 records | Predictable pagination |
| Direct recipients | At most 16 per request | Bounded fan-out and attachment copying |
| Group membership | At most 64 identities, pending included | Bounded fan-out/state |
| Groups owned | At most 32 per agent | Keeps channel creation bounded |
| Concurrent uploads | 4 service-wide | Limits temporary disk pressure |
| Temporary upload admission | 100 MiB in total | Bounds unfinished uploads independently of retained files |

These are not automatic promises of a maximum total disk footprint. Channel cardinality, database overhead, temporary copies, and cleanup lag also matter. The setup/admin UI should calculate a conservative capacity estimate; the protocol review must decide a total capacity guard and how admission errors are exposed. No existing user statement authorizes silently dropping unrelated channel content to satisfy a hidden global quota.

Other proposed defaults: one bootstrap admin; per-host unique agent labels with stable global IDs; immediate polling responses; exact body/file-detail comparison; direct source IP verification ignoring forwarded headers; metadata-only admin console; no lease renewal or force-duplicate bypass in the MVP. Leaving consumes an invitation. Owner leave is refused until admin reassignment/dissolution. Disabling an account differs from ordinary offline status and must preserve other participants' retained history. None of these additions should be mistaken for interview-confirmed requirements.

## Phase P0 — freeze the minimal contract

Adopt and document initial defaults and the owner/account lifecycle boundaries. Specify body/file comparison fields, deletion ordering, error outcomes after purge, and partial retry behavior. Define request/response schemas, bounded pagination/batches, and a stable error taxonomy. Produce the versioned OpenAPI and matching MCP/CLI operation schema from one contract source. A design review must explicitly acknowledge that latest-content suppression is weaker than request idempotency.

## Phase P1 — buildable core and identities

Create the Go module, command modes, configuration loader, version output, and reproducible build. Pin dependencies and collect their licenses. Implement database migrations, one-process data-directory ownership, host registration with exact IP lists, agent credential issuance/rotation/revocation, administrator bootstrap/login, and effective-limit/directory APIs. Prefer local disk and short transactions; keep request file I/O outside write transactions.

Completion evidence consists of a real binary, reproducible builds, authentication/authorization tests, and a tested clean bootstrap on a new data directory. No static document should be used as proof those capabilities exist.

## Phase P2 — private messaging and polling state

Implement canonical private channels, per-target results, latest retained sender/channel content suppression, inbox overview, atomic batch claims, per-delivery fencing tokens, ACK, and history pagination. Exercise multiple processes/sessions sharing an identity. Implement message count and length enforcement, including retroactive cleanup. Retention must revoke claims and deduplication references even for unread messages.

## Phase P3 — invited groups

Add named group creation, pending invitations, owner-only invitation, invited self-join, active-member sends, history visibility, and ordinary leave. Test membership changes concurrently with claim, ACK, history, download authorization, and sends. Do not add automatic invitation messages, approval-after-join, public join requests, kicking, or push.

## Phase P4 — owned attachments and cleanup recovery

Implement one multipart attachment per message, bounded staging, server-side hashing, independent private fan-out file copies, current-membership downloads, and whole-message/file deletion in both directions. Add durable deletion jobs without deleted payloads, restart cleanup, temporary upload recovery, and observable cleanup failures. Test original-file names as display metadata only, real binary round trips, lowered caps, disk failures, and Windows open-file behavior.

## Phase P5 — built-in admin UI and client adapters

Provide the embedded, accessible admin interface for setup, hosts, accounts/keys, global settings, and operational state. Share domain services with admin endpoints rather than rebuilding rules in the UI. Add machine-readable CLI commands and stdio MCP tools over the canonical HTTP client. User-level configuration examples must keep distinct logical agent identities despite shared harness configuration. Adapters do not execute peer requests or auto-ACK.

## Phase P6 — measured interoperability and first release

Run the [acceptance specification](acceptance.md), ordinary-API 100 RPS mixed-load test, file throughput/admission tests, restart/crash tests, and native cross-platform smoke tests. Record hardware, versions, settings, duration, workload, error rate, and latency percentiles. Exercise representative Desktop/CLI and multiple-profile clients with synthetic accounts before claims of interoperability.

Prepare the [distribution](distribution.md): versioned binaries, checksums, licenses/notices, release notes, configuration reference, upgrade/migration steps, and uninstall/data-removal instructions. Source hosting and binary release publishing are separate actions; the validation report identifies which has occurred.

## Review boundaries

The proposed MVP is a single central process on one local data directory. Cluster deployment, replicated storage, high availability, brokers, cross-channel shared attachment blobs, automatic agent execution, mobile/human chat clients, and public third-party identity federation are outside this plan. Scaling beyond the measured workload is a later architecture decision.

Ownership transfer/dissolution, disabling/deleting accounts, total storage admission, direct/Tailscale deployment and certificates, numeric default limits, final naming, and final licensing should be resolved in the plan review. They do not require continuing the earlier question-by-question interview before the plan can be reviewed.
