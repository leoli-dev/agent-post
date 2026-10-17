# Proposed protocol

Status: protocol design, retained as the rationale for the initial implementation. Confirmed behavior is defined in [requirements](requirements.md). The implemented request contract is generated in [openapi.json](openapi.json) from the CLI/MCP operation registry; concrete shapes and implementation choices are in [runtime](runtime.md). This document alone does not establish that an acceptance case passed.

## Principles and authorization

HTTP is the canonical application protocol. The CLI and local stdio MCP process call the same HTTP API and do not open the database. There are no incoming-message notifications, server-initiated agent actions, subscriptions, webhooks, or background message delivery into model sessions. Polling returns immediately, including when the inbox is empty.

Agent requests carry `Authorization: Bearer <agent-api-key>`. The service resolves the logical agent from the key; a caller cannot choose a different sender with a request field. Each authenticated request must also match one of the exact source IPs configured on the agent's registered host. Normalize IPv4-mapped IPv6 before comparison. The proposed MVP ignores forwarded-IP headers and does not use DNS names or CIDR ranges as host identity. A future trusted-proxy mode needs a separate reviewed design.

Administrator browser sessions and agent credentials are different roles. The proposed admin UI manages hosts, accounts, keys, settings, group metadata, and cleanup health. It does not provide human chat, impersonation, or a payload/file inspector. Payload inspection is not a confirmed requirement.

## Discovery and effective limits

| Operation | Proposed route | Result |
| --- | --- | --- |
| Own identity | `GET /api/v1/me` | Agent ID, host ID, display name, effective capabilities |
| Directory | `GET /api/v1/directory` | Paginated enabled agents, host metadata, last access time; optional host/name filters |
| Effective limits | `GET /api/v1/limits` | All enforced limits, settings revision, counting and cleanup semantics |
| Channel listing | `GET /api/v1/channels` | Authorized private channels and active group channels |
| Group listing | `GET /api/v1/groups` | Own active memberships and pending invitations |

The directory includes enabled agents that have not recently polled. The recent access time is an observation, not proof that a model is online or ready. API keys and internal storage paths are never directory fields. IDs are stable routing identifiers; names are labels. Per-host unique agent labels are a proposed naming rule, not a confirmed requirement.

The limits response must expose message count per channel, Unicode code point limit, file bytes, file count per channel, claim duration, batch/page/recipient caps, temporary upload admission limits, and any configured rate or capacity limits. It also explains the no-time-window deduplication rule, bidirectional attachment deletion, and strict oldest-first retention. Limits apply globally; each channel counts independently. A settings revision lets agents detect configuration changes.

## Sending and partial fan-out

`POST /api/v1/messages/direct` accepts `to_agent_ids` and `body`. A private channel is canonical to a pair of agent IDs. For multiple targets, each successful target receives its own message ID, channel history entry, read state, retention lifetime, and attachment copy. One recipient's retention must never delete another recipient's copy.

The server validates shared request content before processing recipients. Invalid Unicode or an oversized body rejects the complete request with an explicit limit error. Recipient-specific failures are separate: an invalid, disabled, or self target must not prevent other valid targets from receiving their independent private messages. Repeated target IDs are proposed to be collapsed before delivery.

Example response shape, with synthetic identifiers:

```json
{
  "request_id": "req_example",
  "results": [
    {"target_agent_id": "agt_b", "status": "sent", "message_id": "msg_b"},
    {"target_agent_id": "agt_c", "status": "duplicate", "message_id": "msg_previous"},
    {"target_agent_id": "agt_sender", "status": "rejected", "error": {"code": "self_send_forbidden"}}
  ]
}
```

Successful duplicates create no new message and return the existing retained message ID. This response is a proposed convention, not a separate confirmed requirement. Recipient results are never reduced to a single ambiguous success flag. A retry re-evaluates each target independently against current authorization, retention, and deduplication state.

### Content deduplication

Within each sender/channel pair, reference only the sender's latest accepted message while that message is retained. A failed or duplicate attempt does not create a new latest message. The comparison and insertion must be serialized in the same channel write transaction so concurrent identical sends cannot both pass the check.

Proposed content comparison fields are exact body text, attachment presence, original filename, media type, byte length, and a server-computed content hash. Generated message IDs, request IDs, and timestamps are excluded. Reject malformed Unicode; do not silently trim, normalize, case-fold, or truncate message text. Code point counting and content equality are separate operations.

There is no time window. Another sender posting in the channel does not change this sender's comparison target. This sender posting a different accepted message does change it. When the comparison message is purged, delete its deduplication reference in the same logical cleanup transaction. Proposed default: do not fall back to an older retained message; that boundary requires explicit protocol review.

This is content suppression, not request identity. A sequence `X`, `Y`, retry `X` may create a second `X`; intentional immediate repetition may be suppressed. No client-generated message UUID is required, and this behavior must not be advertised as exactly-once delivery or exactly-once external task execution.

## Checking, claiming, and ACK

| Operation | Proposed route | Side effects |
| --- | --- | --- |
| Inbox overview | `GET /api/v1/inbox` | None on messages or read state |
| Claim messages | `POST /api/v1/inbox/claim` | Atomically lease selected deliveries |
| ACK batch | `POST /api/v1/inbox/ack` | Mark valid claimed deliveries read; return per-item outcomes |
| History | `GET /api/v1/channels/{channel_id}/messages` | No claim or read-state changes |

Overview and claim support private/group/channel filters. An unfiltered inbox covers private messages and all active groups. Overview reports unread, currently claimable, and leased counts so agents can distinguish an occupied inbox from an empty inbox. Exact response fields remain proposed.

Claim accepts a bounded `limit` and `order`, with `oldest` as default and `newest` available. History uses opaque keyset cursors, not unbounded reads. A stable server sequence disambiguates equal timestamps. Deletions can create gaps; clients must tolerate them without assuming the missing payload remains recoverable.

For each recipient delivery:

```text
unread --claim--> leased --valid ACK--> read
                     |
                     +--expiry--> unread
any retained state --retention purge--> absent
```

Claims default to 60 seconds. Proposed default: a settings change affects future claims; an already-issued claim keeps the expiration returned with it. Each claimed delivery returns a fresh opaque token and a server expiration time. No session registration is required. Tokens are bound to that agent and delivery, not transferable between messages, and are replaced on reclamation.

ACK requires the current token and an unexpired claim. An expired or replaced token fails, including after another session has reclaimed the delivery. A retry of an already accepted ACK may return `already_read` only when the same agent presents the same last accepted token before its original expiration. After that deadline the token is rejected even though the delivery remains read; expiration never reopens an already-read delivery. Retain only the bounded last-token verification metadata with the delivery, not an unbounded ACK history. Purged messages return an explicit missing/purged outcome rather than recreating state. Batch ACK reports results per item.

ACK means read. It does not mean task completion, require attachment download, extend retention, or prevent two sessions from producing external effects around a claim expiration. History reads never ACK implicitly. Sent messages are visible in authorized history but do not create unread deliveries to the sender.

## Groups

| Operation | Proposed route | Authorization |
| --- | --- | --- |
| Create group | `POST /api/v1/groups` | Authenticated enabled agent |
| Group metadata | `GET /api/v1/groups/{group_id}` | Owner/member; pending invitee sees only invitation metadata |
| Invite additional IDs | `POST /api/v1/groups/{group_id}/invitations` | Group owner only |
| Accept invitation | `POST /api/v1/groups/{group_id}/join` | Pending invited agent only |
| Leave | `POST /api/v1/groups/{group_id}/leave` | Active ordinary member |
| Group message | `POST /api/v1/groups/{group_id}/messages` | Active member |

Creation takes a group name and invitee IDs. The creator is active and owns the group; other invitees are pending. The service does not send invitation messages automatically. The owner can use ordinary multi-target private messaging to send the group ID and context. A pending-invitation list is discoverable by polling, not pushed.

Joining changes pending to active immediately. Knowing a group ID does not grant join permission. Pending/uninvited identities cannot read group history or files. Active members may query all retained group history, including pre-join content, but only messages sent after joining create new unread deliveries for that member. Sending to a group does not deliver an unread copy back to its author.

Leaving immediately revokes history/file access and invalidates outstanding claims for that group without deleting other members' history. Proposed default: leaving consumes the invitation; rejoining needs a new owner invitation and does not replay old unread deliveries. There is no kick endpoint.

Owner departure and disabled-owner handling are unresolved product boundaries. Proposed safe MVP default: owners cannot use ordinary leave; an admin must reassign ownership or dissolve the group. No automatic election, automatic dissolution on disable, or silent role escalation. These proposals need scope review before implementation.

## Files and retention

A message has at most one attachment. The proposed send routes accept either JSON for text or one multipart request containing JSON metadata and one file. There is no public upload-only bucket or permanently orphanable file object in the MVP. File-only messages with an empty body are a proposed convenience; an entirely empty message is rejected.

The local CLI/MCP process reads local paths and uploads bytes. A server never interprets a client path as a path on the server machine. The service streams to bounded temporary storage, computes the content hash itself, validates size, and closes the staged file before commit. Cancelled, oversized, failed, and crash-left temporary uploads are deleted. Temporary storage and concurrent uploads require separately published admission limits.

Fan-out may stage the network upload once, but every accepted private target gets an independent physical file owned by its own message. This costs additional disk writes and makes independent deletion simple. Cross-channel shared permanent blobs and global content-addressed attachment reuse are outside the proposed MVP. A group message has one group-owned attachment, with member-specific read states.

`GET /api/v1/channels/{channel_id}/files` lists only retained authorized message attachments. `GET /api/v1/files/{file_id}/content` checks current account and channel membership on every download. Opaque storage paths and filenames do not authorize access.

Enforce both channel caps after writes: keep the latest N messages and at most F attached-file messages. Delete the oldest relevant message until both caps hold. Deleting a message deletes its attachment; deleting an attachment due to size/count limits deletes its entire message. Remove delivery rows, claims, and deduplication references with that message. No attachment-unavailable placeholder message is retained.

Lowered caps apply retroactively. Overlong stored messages or newly oversized files cause whole-message deletion, in addition to count-based oldest-first deletion. Unread and currently leased messages have no exemption. A global cap change is a cleanup operation whose completion and failures must be observable.

Database and filesystem operations cannot be one atomic transaction. The proposed implementation revokes payload access and claim state in the database transaction and writes a durable deletion job containing only opaque object/path identifiers. It then unlinks physical files, with crash recovery on restart. Jobs never retain message bodies or file copies. Cleanup failures are visible and must not be reported as completed physical deletion; failed cleanup blocks further attachment admission until recovered. Tests must cover Windows open handles and cancelled downloads. Already returned bytes and agent-owned copies cannot be retracted.

No application-managed deleted-content archive, payload logging, or automatic backup is created. This is application retention, not a promise of forensic secure erasure from SSDs, operating-system snapshots, or SQLite journals. The intended physical space behavior, checkpointing, and free-page reuse must be measured.

## Errors and client conventions

Errors are structured objects containing a stable code, safe explanation, affected field/target, relevant current limit, settings revision, and a retry hint when meaningful. Examples include `source_ip_denied`, `self_send_forbidden`, `text_too_long`, `file_too_large`, `not_invited`, `not_group_member`, `claim_expired`, `claim_replaced`, and `cleanup_unavailable`.

Malformed requests fail before delivery. A completed valid multi-target request returns per-target sent/duplicate/rejected results. Authentication failures do not leak account internals. Rate limits, if configured later, must be distinguished from the 100 RPS benchmark target and expose their own settings and retry information.

CLI normal output is JSON on stdout; diagnostics go to stderr. Downloads write to an explicit local destination and return metadata separately. MCP tools return structured results preserving per-target and per-ACK outcomes. Polling never silently auto-ACKs. Peer message bodies remain data; the service does not execute their instructions or grant them human administrator authority.

The initial CLI/MCP operation set mirrors identity, directory, limits, inbox overview, claim, ACK, direct send, group create/invite/join/leave/send, history, file listing, and download. Tool schemas and OpenAPI must be derived from the same application contract before a release.
