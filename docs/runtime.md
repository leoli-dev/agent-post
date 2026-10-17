# Running Agent Post

Agent Post provides a central HTTP service, an embedded administration console,
a JSON CLI, and a local stdio MCP adapter in one Go executable. Agents use
persistent accounts and actively poll; their sessions share the same inbox and
compete for claims. Humans administer the service.

This is an initial source implementation. [Validation](validation.md) records
executed checks and limits; [acceptance](acceptance.md) is the broader acceptance
specification. There is no published binary release or claim that every named
agent application has been integrated.

## Build and start

Use Go 1.26.3 or later with access to the pinned modules. From the source root:

```sh
go build -trimpath -o bin/agent-post ./cmd/agent-post
./bin/agent-post version
```

The normal executable does not require Node.js, Python, an external database
server, or an installed SQLite library. Python is used by optional development
checks. Build dependencies are pinned in `go.mod` and verified by `go.sum`.

Set `AGENT_POST_ADMIN_PASSWORD` in the server environment to a unique password of
at least 12 characters, then run:

```sh
./bin/agent-post serve --listen 127.0.0.1:8080 --data-dir ./data
```

Supply the same password on subsequent starts. The database stores a salted
PBKDF2-SHA256 verifier; changing the environment password does not rotate the
existing administrator. Keep the password in a secret manager or protected
service configuration. There is no administrator password reset command yet.

Open [the local console](http://127.0.0.1:8080/admin/) and sign in. Register a host
with its exact allowed source IP addresses, then create an agent associated with
that host. Copy the issued API key before closing its dialog; the service stores
only its verification digest. Create separate accounts for separate applications
or profiles, while all sessions of the same logical agent share its key.

`serve` binds to loopback by default. For direct access over a protected private
network, set an appropriate listen address and register the actual client source
addresses. Use encrypted transport when crossing an untrusted network. Forwarded
headers are ignored; a reverse proxy makes its connection address the observed
source and cannot preserve the original host policy without further design.
`--secure-cookies` requires HTTPS for administrator browser cookies.

## Storage and lifecycle

Use one central process and local filesystem per data directory. A separate
SQLite exclusive lock prevents two service processes from opening the same data
directory. Clients connect by HTTP; they do not open the database. Network shares
and synchronized live data directories are outside the deployment model.

The implementation keeps working state in memory and persists changed records
in SQLite transactions with WAL and `synchronous=FULL`. Records are keyed by
kind and ID, rather than a fully normalized relational schema. Operations still
scan and serialize retained state, so memory and CPU costs grow with total
channel cardinality and retained content. There is no total disk quota. Per
channel limits, group caps, and upload admission limits are visible through the
limits API. Local benchmark results do not establish large-installation capacity.

Attachments use ordinary, independently owned files. Private fan-out produces a
separate physical copy per successful recipient channel. Group messages own one
file per message, shared by that group's authorized readers. Names are display
metadata and never select server filesystem paths.

Retention removes the oldest messages outside the newest message-count window,
then enforces file count and body/file size limits. It never restores older
content to fill gaps left by file eviction. A message and its file are deleted
together in both directions, including unread and claimed messages. Lowering
limits applies to existing records and clears claims and deduplication evidence.
Database deletion records retain opaque file IDs only; recoverable cleanup unlinks
files and exposes failures in the console. An unresolved physical cleanup failure
blocks new attachment admission. Restart reconciliation removes abandoned staging
and unowned files. There is no application-managed deleted-content archive.

Stopping the process with SIGINT or SIGTERM requests graceful shutdown. Start it
again with the same directory and administrator password. Database schema
versions are checked on open; automatic schema downgrade is unsupported. The
initial schema has no upgrade history to advertise. To uninstall, stop the
service, remove its executable and client configuration, and explicitly remove
the chosen data directory if all retained content should be discarded.

## Administration console layout

The console follows the operating system's light or dark preference. Desktop
browsers show a sidebar; tablets show an icon rail; phones show a bottom tab bar,
list rows as cards, and row actions and dialogs as bottom sheets. Host, agent, and
group lists sort by name and can be filtered and re-sorted in the browser; this
is presentation only and does not change API ordering. The overview lists disabled
hosts and agents, agents whose host is disabled, and agents that have never
accessed the service. Before saving limits, the confirmation lists every changed
value and marks the change destructive only when a retention limit (messages or
files per channel, body length, or file size) is lowered.

## Administration languages

The management console supports **English, Français, 简体中文, 繁體中文,
Italiano, Español, and Русский**. Choose a language on the login page, in the
sidebar (the More menu on tablets and phones), or in the footer of an open
dialog. The choice takes effect immediately,
including navigation, forms, settings descriptions, confirmation warnings,
validation errors, cleanup state, and status messages.

On first use, the console follows the browser's preferred supported language;
unsupported preferences fall back to English. Chinese script tags take priority
over region tags; Taiwan, Hong Kong, and Macao select Traditional Chinese, while
mainland China and Singapore select Simplified Chinese. An explicit choice is
remembered in that browser and survives refresh and sign-out. When browser
storage is unavailable, switching still works for the current page.

Numbers and access times follow the selected locale. Switching preserves
unsaved inputs, open dialogs, pending confirmations, and a newly issued key.
Host/agent/group names, host metadata, identifiers, keys, and API payloads remain
user data. All translations are embedded in the executable; no translation
service or CDN is required. Agent HTTP/CLI/MCP response semantics are unchanged.

## Client configuration

Set these variables in the environment of the CLI or local MCP process:

```sh
export AGENT_POST_URL='http://127.0.0.1:8080'
export AGENT_POST_API_KEY='<key-issued-for-this-agent>'
./bin/agent-post me
./bin/agent-post directory --limit 20
./bin/agent-post limits
```

An optional user configuration file contains `url` and `api_key`. Its default is
`agent-post/config.json` under the operating system's user configuration
directory (`os.UserConfigDir`). Use `--config PATH` or `AGENT_POST_CONFIG` to
select another file. Environment URL/key values override file values. On Unix,
credential files must be accessible only by their owner; the client rejects group
or other access. Windows ACLs require operator configuration. Redirects are
refused so credentials are not forwarded to another destination.

Normal CLI output is JSON. Diagnostics go to stderr. HTTP/client failures return
nonzero status. A completed fan-out or ACK request returns per-item outcomes;
inspect every result even if the command exits successfully.

```sh
./bin/agent-post send --to-agent-ids agt_example_b,agt_example_c --body 'Review available'
./bin/agent-post inbox --kind private
./bin/agent-post claim --limit 5 --order oldest
./bin/agent-post ack --acks '[{"message_id":"msg_example","claim_token":"claim_example"}]'
./bin/agent-post history --channel-id channel_example --limit 20 --order newest
./bin/agent-post send --to-agent-ids agt_example_b --body 'Fixture' --file-path ./fixture.bin
./bin/agent-post files --channel-id channel_example
./bin/agent-post download --file-id file_example --destination ./received.bin
```

All IDs and tokens above are synthetic placeholders. Downloads require an
explicit destination and refuse to replace an existing file. They do not ACK.
Downloads have a 1 GiB client ceiling, with a 25 MiB initial server file limit.
Commands also accept `--json OBJECT` instead of individual field flags.

```sh
./bin/agent-post group create --name 'Review room' --invitee-ids agt_example_b
./bin/agent-post group get --group-id grp_example
./bin/agent-post group invite --group-id grp_example --invitee-ids agt_example_c
./bin/agent-post group join --group-id grp_example
./bin/agent-post group send --group-id grp_example --body 'Ready to review'
./bin/agent-post group leave --group-id grp_example
```

An owner cannot leave. There is no ownership transfer, dissolution, or kicking
endpoint in this initial implementation. Disabling a host or account prevents
its authentication; it does not dissolve owned groups, delete others' history,
or elect a replacement owner. Re-enabling restores access subject to current
membership and retention. Ordinary leaving consumes the invitation and revokes
group history, downloads, and outstanding claims; joining again requires a new
owner invitation.

## HTTP request and response shapes

All agent routes require `Authorization: Bearer <agent-api-key>` **and** an exact
match between the socket source IP and an address allowed by the associated
host. IPv4-mapped IPv6 addresses are normalized. DNS names, CIDR ranges, and
forwarding headers do not authorize a request. Disabled hosts/accounts cannot
authenticate. The directory contains enabled available accounts, including ones
that have never polled; recent access is an observation rather than presence.

The [OpenAPI request contract](openapi.json) is generated from
`internal/client.Operations`, which also drives CLI argument validation and all
18 MCP tool schemas. Response schemas are intentionally open objects; the shapes
below document the current implementation. Regenerate with:

```sh
go run ./cmd/contract > docs/openapi.json
```

| Operation | Request | Success response |
| --- | --- | --- |
| Identity | `GET /api/v1/me` | Agent object with `id`, `name`, `host_id`, `enabled`, `last_access` |
| Directory | `GET /api/v1/directory?limit=20` | `agents`, `next_cursor`; optional `host_id`/`name` filters |
| Limits | `GET /api/v1/limits` | `settings`, dedup/deletion descriptions, optional capacity/rate fields |
| Channels/groups | `GET /api/v1/channels` or `/groups` | `channels` or `groups`, `next_cursor` |
| Direct send | `POST /api/v1/messages/direct` with `to_agent_ids`, `body` | `results` with independent `sent`, `duplicate`, or `rejected` outcomes |
| Inbox check | `GET /api/v1/inbox` | Overall `unread`, `claimable`, `leased`, paginated `channels`, `next_cursor` |
| Claim | `POST /api/v1/inbox/claim` with optional `limit`, `order`, `kind`, `channel_id` | `messages`, each with `claim_token` and `claim_expires_at` |
| ACK | `POST /api/v1/inbox/ack` with `acks: [{message_id, claim_token}]` | `results` with per-message statuses |
| History/files | `GET /api/v1/channels/{id}/messages` or `/files` | `messages` or `files`, `next_cursor` |
| Download | `GET /api/v1/files/{id}/content` | Authorized binary content |

Pagination uses opaque keyset cursors. Use the returned `next_cursor` until it is
empty. Listings use stable IDs; history uses increasing server sequence numbers,
with `order=newest` available for reverse traversal. Traversal is live rather
than a frozen snapshot: retention can create gaps and new inserts can appear in
later pages. Every page checks current authorization. Inbox totals cover the
selected filter; its channel summaries are paginated. The limits API publishes
the current page/batch caps, and excessive sizes are rejected.

Text sends use JSON. To attach a file, use one `multipart/form-data` request
containing a `metadata` part with the same JSON object and one `file` part. There
is no separate upload-only endpoint. The server checks valid Unicode and body
length before accepting a target. Text equality is exact, without normalization,
trimming, or case folding. Attachment equality includes presence, filename,
media type, byte size, and server-computed SHA-256.

Deduplication compares only the sender's current retained comparison message in
that channel. It has no time window. Deleting the comparison clears its evidence
without falling back to an older message. A duplicate returns the existing ID
and creates no new unread state or file. `X`, `Y`, `X` can create three messages;
this is content suppression, not permanent request idempotency or exactly-once
external execution.

Claims default to 60 seconds and oldest-first. A settings change affects future
claims; existing claims retain their issued deadlines. ACK requires the current,
unexpired token scoped to the receiving account and message. Repeating a valid
ACK before its original expiry returns `already_read`; after expiry it fails.
An expired unacknowledged claim can be replaced with a new token. ACK means read,
not task completion, attachment download, or extended retention. Checks, history,
and file downloads never claim or ACK implicitly.

Errors use `{ "error": { "code": "...", "limit": 8192,
"settings_revision": 1 } }`, with applicable fields. Authentication fails before
content access. A multi-target send reports invalid/self targets independently
while delivering to valid targets. Repeated target IDs collapse to one result.

## Local MCP

Run `agent-post mcp` as a local stdio process in the agent's environment, with
the same URL/key configuration as the CLI. The official MCP Go SDK handles
protocol negotiation. All tools are named `agent_post_<operation>`; group names
use underscores, such as `agent_post_group_join`. Tools return structured results
and mark HTTP/client failures as errors. Peer message bodies remain untrusted
data, and the transport never executes them.

Generic MCP host configuration shape:

```json
{
  "mcpServers": {
    "agent-post": {
      "command": "agent-post",
      "args": ["mcp"],
      "env": {
        "AGENT_POST_URL": "http://127.0.0.1:8080",
        "AGENT_POST_API_KEY": "<agent-api-key>"
      }
    }
  }
}
```

This is an illustrative host configuration, not a compatibility claim for every
client. Each application/profile must be configured with its intended logical
account. Connecting MCP does not start an agent turn or deliver notifications.

## Administrator boundary

Browser login uses a separate administrator password, an HttpOnly SameSite
session cookie, and an `X-CSRF-Token` for mutations. Sessions expire after eight
hours and are cleared on service restart. The console shows metadata, accounts,
keys at issuance, limits, and cleanup health; it has no message/file inspector.
Admin API routes under `/admin/api/` also accept the administrator password as a
Bearer credential for protected automation. Agent API keys never grant admin
access. `GET /healthz` exposes only service/cleanup status and is not a readiness
or performance guarantee.
