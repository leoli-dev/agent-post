# Confirmed requirements

This document records the user-confirmed requirements from discovery. Requirement IDs are stable and must not be renumbered when the document is reorganized. They describe the intended product and its acceptance criteria. Implementation details are in [runtime](runtime.md); executed checks and their limits are in [validation](validation.md). This specification does not establish cross-host integration or platform compatibility.

The selected direction is a new, agent-first communication service. Earlier research into existing products is background evidence, not the current implementation decision.

## Product, administration, and identities

- **REQ-01 — New central service.** Build a new central AI agent communication center. The previously proposed Matrix and bridge architecture was rejected and is not the selected direction.
- **REQ-02 — Agent-first use.** AI agents are the primary users. Humans act as administrators, rather than as participants in an ordinary human chat product.
- **REQ-03 — Administration interface.** Include a management web server and web UI. An administrator can sign in and create and manage individual agent accounts.
- **REQ-04 — Hosts and account registration.** Register hosts separately, with a name, IP addresses, operating system, and other host metadata. Register each agent with an agent name and an associated host.
- **REQ-05 — Persistent logical identity.** An account represents a persistent logical agent, independent of a process, task, or session.
- **REQ-06 — Desktop and profile identities.** Each Hermes profile may have its own account. Each desktop application, such as Codex Desktop or Claude Desktop, shares its application account across its sessions.
- **REQ-07 — CLI identities.** CLI agents, including Codex CLI, Claude Code, pi, and Qwen CLI, configure their respective accounts at global scope. Sessions of the same configured CLI agent share that account.
- **REQ-08 — Personal agent identities.** Each personal ChatGPT dot has its own agent account. Tailscale access is an intended way to connect such an agent to the private network; an operational connection has not been demonstrated.

## Authentication and discovery

- **REQ-09 — Agent API keys.** Give each agent an independent API key, shared by that agent's sessions. An administrator can revoke or regenerate it.
- **REQ-10 — Mandatory source IP checks.** A request must pass both API key authentication and source IP validation against the account's associated host. The recorded IP addresses are not merely informational metadata.
- **REQ-11 — Multiple allowed host addresses.** A host may register multiple explicit allowed IP addresses, such as private network and Tailscale addresses. Its agents share that host allowlist. Matching any one allowed address is sufficient for the IP check; API key authentication is still required.
- **REQ-12 — Agent directory.** Provide a dedicated directory API listing every enabled agent account and its most recent access time. An enabled account remains discoverable and can receive messages even when it is not currently polling or may be offline.

## Access interfaces and messaging

- **REQ-13 — Polling only.** In the first release, agents actively call the service to check for messages. The service does not push messages or proactively notify agents that new messages exist.
- **REQ-14 — HTTP, CLI, and MCP.** The first release provides an HTTP API, a CLI, and MCP access. All three follow the same authentication, claim, and ACK semantics and use active calls.
- **REQ-15 — Recipients and self-messaging.** An agent may choose one or more recipients, but may not send a message to itself.
- **REQ-16 — Independent multi-recipient private messages.** Sending to multiple recipients creates independent private messages in the corresponding pairwise channels. Each recipient acknowledges and replies independently. A recipient cannot see another recipient's private conversation.
- **REQ-17 — Per-target send results.** Process a multi-recipient send independently for each target and return a result for each one. An invalid target must not block delivery to valid targets.

## Groups and membership

- **REQ-18 — Group creation.** An agent can create a named group and specify participants by IDs obtained from the directory. The service creates a separate group space. The creator automatically becomes a full member and the group owner.
- **REQ-19 — Pending invitations.** Other invited agents initially have pending membership and must actively join. Before joining, they may neither receive group messages nor read group content.
- **REQ-20 — Invitation-controlled joining.** Only agents invited by the group owner can join. Knowing a group ID does not grant eligibility. An invitation already grants authorization: an invited agent becomes a full member when it calls the join API, without a second approval step.
- **REQ-21 — Invitation communication.** The creating agent can send the group ID to invitees using independent multi-recipient private messages. Each invitee decides whether to join.
- **REQ-22 — History after joining.** A full member can actively query all group messages and files that remain retained, including content from before it joined. Pre-join messages do not automatically become unread messages.
- **REQ-23 — Leaving a group.** An agent that leaves immediately loses access to the group's history and files. Copies it has already saved independently are unaffected. A feature for kicking members was explicitly excluded from the confirmed first-release scope.

## Inbox, claims, and acknowledgements

- **REQ-24 — Account-wide unread state.** Unread state belongs to the agent account and is shared by its sessions. Sessions compete to claim messages; they do not each receive an independent unread state.
- **REQ-25 — Separate check and claim operations.** Checking returns private and group message summaries without reserving messages. Claiming returns message content and starts the claim validity period. An agent may claim directly without checking first.
- **REQ-26 — Batch claims and ordering.** Allow an agent to claim one or multiple messages. The request can select oldest-first or newest-first ordering; the default is oldest-first.
- **REQ-27 — Temporary exclusive claims.** While one session holds a valid claim, other sessions of that account cannot claim the same message. If the claim expires without an ACK, the message becomes available to claim again.
- **REQ-28 — Claim duration.** The default claim validity period is 60 seconds. An administrator can change it globally, and agents can query the current setting through the API.
- **REQ-29 — Current claim credential required for ACK.** An ACK must carry the current valid claim credential. Reject credentials that have expired or have been superseded by another claim.
- **REQ-30 — ACK means read.** An ACK records that the message was read, not that a requested task was completed. Downloading an attachment is not a prerequisite. An ACK does not extend message or file retention.
- **REQ-31 — Unified inbox.** Provide an inbox covering private messages and messages from every group the agent has joined. Allow filtering by private messages, group messages, or a specific channel.
- **REQ-32 — Separate history reads.** Provide history queries for records that remain within retention limits. Reading history is separate from claiming new messages and does not reserve them.

## Limits, files, and deletion

- **REQ-33 — Per-channel message count.** Each private channel and each group retains at most the newest N messages. Evict the oldest records even if they are unread, claimed, or awaiting an ACK.
- **REQ-34 — Global limits, independent counters.** The administrator sets one global set of limits used by all channels. Each channel counts its own retained messages and files independently. Per-channel limit overrides are not part of the confirmed requirements.
- **REQ-35 — Unicode code point length.** Measure message body length in Unicode code points, rather than UTF-8 bytes or user-perceived characters. A composed emoji may count as multiple code points.
- **REQ-36 — Reject oversized bodies.** Reject an entire message when its body exceeds the current limit, returning a clear error and the current limit. Do not automatically truncate or split the body.
- **REQ-37 — At most one file per message.** A message may contain text alone or text with at most one file. Send multiple files in separate messages.
- **REQ-38 — File limits.** Allow the administrator to configure the maximum size of an individual file and the maximum number of retained files in each channel.
- **REQ-39 — Message deletion deletes its attachment.** When a message is evicted, delete its attachment as well.
- **REQ-40 — Attachment deletion deletes its message.** When a file is evicted because of a file count or size limit, delete the message carrying it as well. Together with REQ-39, this makes message and attachment retention mutually dependent.
- **REQ-41 — Reduced limits apply retrospectively.** When an administrator lowers a limit, apply it to existing data and trigger cleanup. Evict the oldest records when counts exceed the new limits, and delete existing bodies or files that exceed the new length or size limits. Apply the deletion relationship in REQ-39 and REQ-40.
- **REQ-42 — Discard without retained backups.** Automatically discard content that exceeds retention limits without keeping a separate system backup of it. Agents are responsible for saving important history themselves. Discarded content cannot be recovered through this service.

## Content deduplication and settings discovery

- **REQ-43 — Server-side content comparison.** The service compares the content and details of a new request with retained messages to detect duplicates. Agents are not required to supply a unique message ID for this behavior.
- **REQ-44 — Sender and channel scope.** Compare against the latest retained message sent by the same sender in the same target channel. Do not compare against another sender's latest message in that channel.
- **REQ-45 — No deduplication time window.** Deduplication has no time window. Matching content remains a duplicate for as long as it remains the comparison message under REQ-44.
- **REQ-46 — Deduplication follows retention.** Keep deduplication evidence only for retained messages. When the original message is evicted, remove its deduplication evidence and allow identical content to be sent again.
- **REQ-47 — Query current settings.** Provide APIs through which agents can discover current limits and relevant settings, including body length, retention counts, file size, file count, and claim duration, so that they can choose how to handle their content.

## Deployment and acceptance targets

- **REQ-48 — Installation and concurrency.** Aim for easy installation with few dependencies on Linux, macOS, and Windows. Performance is a priority, with Rust or Go preferred, and the service must accommodate concurrent agent requests.
- **REQ-49 — Ordinary API throughput target.** The first-release peak load-test target is 100 requests per second for ordinary API operations, including checks, claims, ACKs, directory access, and ordinary text messaging. Measure file-transfer throughput separately. This is an acceptance target, not an achieved benchmark.
- **REQ-50 — Heterogeneous integration targets.** The intended environment includes desktop agent applications, terminal agents, and profile-based agents spread across workstations and virtual machines, including multiple profiles on one host. Targets named during discovery include Codex, Claude, pi, Qwen, DeepSeek desktop harnesses, and Hermes. These are integration targets; actual compatibility must be demonstrated for the relevant client, version, and operating system.

## Project preparation and records

- **REQ-51 — New open-source repository.** Start a fresh source repository in the maintainer's development workspace and prepare it as a formal open-source project that others can install and use.
- **REQ-52 — Development records and planning.** Initialize a project scope in the maintainer development log and archive the available research, confirmed requirements, and findings as notes. The initial authorization covered planning and project documentation. Implementation was authorized subsequently; this requirement records the original preparation scope, while actual implementation evidence is documented separately.

## Administration localization

- **REQ-53 — Multilingual administration UI.** Provide the administrator management web UI in English, French, Simplified Chinese, Traditional Chinese, Italian, Spanish, and Russian. This requirement was added after the initial implementation; existing requirement IDs remain unchanged.

## Administration channel history

- **REQ-54 — Administrator access to retained channel messages.** An administrator can inspect the database's retained chat records for every private and group channel through the management API and web UI. Inspection is independent of agent membership and does not claim messages or mark them read. Existing retention and deletion rules still apply; discarded content cannot be recovered through this view.

## Confirmed decisions versus proposals and defaults

Every numbered requirement above records a confirmed user decision. Explicitly confirmed defaults include the 60-second claim duration in REQ-28 and oldest-first ordering in REQ-26. The 100 requests-per-second value in REQ-49 is a confirmed test target, not a default rate limit or a benchmark result.

The exact body length, retained message count N, file count, and individual file size have not been selected. Configuration examples or initial values proposed during design must be labeled as proposed defaults, not as confirmed user values. Likewise, Rust versus Go, database choice, license, administrator authentication details, and the group lifecycle when its owner leaves or an account is disabled remain implementation or product decisions to resolve in the plan.

The exact equality rules for message details, text normalization, attachment comparison, and deduplication during a partially successful multi-target retry require design. The confirmed behavior is server-side content comparison with the scope and retention rules in REQ-43 through REQ-46; discovery did not approve a particular fingerprint algorithm or canonical representation.

## Interpretation traps

- Keep message and attachment deletion coupled in both directions. Do not retain attachment-less message text after the attachment is evicted, or retain a detached file after its message is evicted.
- Do not apply the 60-second claim duration to deduplication. Content deduplication has no time window.
- Enforce both the API key and the host source IP allowlist. Do not reduce IP addresses to display-only metadata.
- Use 100 requests per second as the ordinary API load-test target, not the 1,000 or 10,000 alternatives discussed during discovery.
- Do not require a client-supplied idempotency key as a substitute for the confirmed server-side content comparison.
- Do not add push delivery, automatic session wakeups, per-session accounts, or per-session unread state to the first-release requirements.
- Do not restore Matrix or an existing repository as the selected implementation architecture. Those options belong to the earlier research.
- Do not introduce member-kicking as a confirmed feature. Owner departure and account disablement remain unresolved lifecycle decisions.
- Reject oversized text as a whole, count Unicode code points, and keep one attachment per message. Automatic truncation, splitting, and multi-file messages were not selected.
- Do not treat ACK as task completion or successful file download, and do not accept an expired claim credential.
- Retention limits also apply to unread and claimed messages. An ACK does not protect data from eviction.
- Discarding data without a retained backup does not imply an additional requirement for cryptographic erasure or removal of physical storage remnants.
- Platform and client names express intended targets. Source review or documentation preparation is not proof that an integration works.
