# Acceptance specification

This document specifies future tests for agent-post. It is not a test report: no application, cross-host integration, or benchmark result is established by these scenarios. The authorized project stage is planning and documentation.

Requirement references below point to [confirmed requirements](requirements.md). Acceptance case IDs identify scenarios, not additional user requirements. A **proposed** behavior, fixture, method, or target is a planning recommendation and has not been confirmed by the user. Where a proposed behavior is not selected, update its scenarios before implementation.

## Scope and evidence

The first release is a central service with an administration UI and active HTTP, CLI, and MCP access. It uses polling; it does not push, wake an agent, inject a turn into an existing session, or promise completion of a requested task. ACK means read. No scenario requires arbitrary external actions to execute exactly once.

Future results should record the application revision, client revision, effective configuration, fixture setup, requests and responses with secrets removed, clock control, and any database or managed-file assertions used. Record a case as executed only when the corresponding evidence exists. Documentation and source review are not integration or performance evidence.

Use isolated test data and a controllable server clock for expiration tests. Fixture values such as three retained messages or a two-message batch are test inputs, not proposed product defaults.

## Fixtures and proposed protocol choices

Create two registered hosts with different explicit source IP lists and at least four enabled logical agents: sender S, recipients B and C, and group owner O. Run two independent sessions of B using B's single shared API key. Register additional accounts only where a scenario needs them. A private channel belongs to a particular pair of agents; a group is a separate channel.

The following details remain proposals rather than confirmed requirements:

| Proposed choice | Consequence for future tests |
| --- | --- |
| Fence every claim using a credential unique to that claim generation. | A credential from an expired or superseded generation cannot ACK, release, or alter a later claim. The concrete credential format and error codes remain protocol choices. |
| A check, history read, attachment download, or repeated poll does not renew a lease. | Test expiration against the original server-issued deadline. Any explicit renewal feature would need a separate decision. |
| Compare text exactly as its Unicode code point sequence, without trimming, Unicode normalization, or newline rewriting. Compare the single attachment by byte content and the documented user-visible file details. | A future protocol must enumerate which details participate in equality; filename and media type behavior cannot be inferred from a hash alone. |
| Keep one latest comparison candidate per sender and channel. If that candidate is purged, clear the candidate without falling back to an older retained record. | This resolves the interaction between REQ-44 and REQ-46 for planning; the no-fallback interpretation is not itself a confirmed user decision. |
| Materialize a separate message and an independently owned physical file copy for every successful private target. | This is a proposed MVP implementation of confirmed independent retention. No shared attachment object or hard link may couple different recipients' deletion lifecycles. |
| Ignore client-supplied forwarded-address headers and use the actual connection source address for IP matching. | Tests must prove that spoofed forwarding headers cannot bypass REQ-10 and REQ-11. A future trusted-proxy mode would require its own explicit configuration and tests. |
| Cap claim batches and history/directory pages; reject an excessive requested size with a clear machine-readable error. Use stable cursor ordering with a unique tie breaker. | Exact cap values, cursor format, and rejection codes remain proposed API details. Do not expose an unbounded escape path through a wrapper. |

Owner departure, account disablement, exact equality fields, administrator authentication, optional rate limits, and numeric limits other than the confirmed lease default still need plan decisions. Tests for those choices must not be presented as confirmation that the user selected them. In particular, do not resolve owner departure by adding member-kicking.

## Administration, identity, authentication, and discovery

Requirement categories: REQ-03 through REQ-12, REQ-47, and REQ-50.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| ID-01: persistent shared account | Start two B sessions, restart one process, and access the service from both with B's account/key. | Both address the same logical account, channels, and unread state. A session or process restart does not create another account or inbox. |
| ID-02: separate profiles and applications | Configure two Hermes profiles and two desktop/CLI identities with different accounts. | Messages and credentials remain isolated by account. Multiple sessions of the same configured application or CLI still share that application's account. |
| AUTH-01: key and IP are both mandatory | Try a valid key from an unlisted source; an invalid key from a listed source; and a valid key from a listed source. | Only the last combination authenticates. There is no successful fallback based on IP or key alone. |
| AUTH-02: explicit multiple addresses | Allow one LAN IP and one Tailscale IP for a host. Connect from each and from a neighboring unlisted address. | Either listed address is accepted with a valid agent key; the neighboring address is rejected. No subnet or prefix is implicitly authorized. |
| AUTH-03: spoofed forwarding headers | From a disallowed source, supply allowed addresses through `X-Forwarded-For`, `Forwarded`, and other plausible proxy headers. | The request remains denied under the proposed direct-source policy. From an allowed source, an untrusted header cannot change the authenticated source identity. |
| AUTH-04: key rotation and shared host list | Revoke/regenerate an agent key; modify its host's explicit address list; access through different sessions of two agents on that host. | A revoked key no longer authenticates. Both agents use the host's updated allowlist, while neither agent's key authenticates as the other. |
| DIR-01: enabled does not mean online | Query the directory while an enabled recipient has not polled for a long period; send it a private message. | The directory includes the account with its recorded most recent access time. Lack of polling does not make it an invalid recipient. |
| SETTINGS-01: discover effective settings | Query body length, message retention count, single-file size, file retention count, and claim duration before and after an administrator changes them. | APIs return current effective global settings, including the confirmed default lease of 60 seconds before an override. No per-channel limit override is implied. |

## Private messages, fanout, and partial sends

Requirement categories: REQ-15 through REQ-17, REQ-24, and REQ-33 through REQ-46.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| DM-01: independent pairwise conversations | S sends one request to B and C. B replies, ACKs its message, and reads its history. | Separate S/B and S/C messages and channels exist. B cannot inspect C's private conversation, ACK C's copy, or affect C's unread state. C's reply and ACK are independent. |
| DM-02: partial send | Include a valid enabled target, an invalid target, and S itself in the recipient list. | The valid target receives its private message. Each rejected target has an explicit failure result. Self-send is rejected and does not prevent delivery to the valid target. No all-target success is falsely reported. |
| DM-03: partial retry | A send succeeds for B but fails for C. Make C eligible, then retry the identical request while B's comparison message remains retained. | Results are evaluated per channel: B is deduplicated against its existing message; C can receive a new message if its own channel has no matching comparison message. The failed C attempt did not advance C's dedup candidate. |
| DM-04: independent retention | With a small global per-channel count, fill S/B until B's fanout copy is evicted, without filling S/C. | Only S/B loses that message. S/C retains its independently addressable message, unread state, claim eligibility, and attachment. The service must not share a deletion lifecycle across private recipients. |
| DM-05: independent physical files | Send one file to B and C, then exercise count eviction and a channel-local file deletion in S/B. | Under the proposed MVP storage choice, each successful target owns a distinct managed file copy with identical bytes and SHA-256. There is no shared blob or hard link whose deletion removes C's file. Permanent storage accounting includes both copies; staging cleanup leaves neither orphan files nor a third permanent copy. |
| DM-06: target-level materialization failure | Inject an allocation or file-copy failure for one private target after staging a multi-target upload. | Successful targets retain complete message/file pairs. The failed target has neither an attachment-less message nor an orphan permanent file; its result reports failure. Staging is cleaned after the request or recovered after a crash. This fault-handling method is proposed, while independent results are confirmed. |

## Groups and authorization

Requirement categories: REQ-18 through REQ-23 and REQ-31 through REQ-32.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| GROUP-01: creator and pending invitees | O creates a named group with B and C invited, then sends a group message. | O is an owner and full member. B and C remain pending until each actively joins. They receive no group inbox message and cannot read the group message, history, or files while pending. |
| GROUP-02: no automatic invitation DM | Create a group with invitees but do not send any private message. Inspect all pairwise channels and inboxes. | Group creation does not automatically send private invitation messages. O may separately send the group ID using ordinary independent DMs. |
| GROUP-03: invitation controls joining | An uninvited agent learns the group ID and attempts to join; pending B calls join. | The uninvited account is denied. B becomes a full member without a second owner-approval step. Possession of the group ID does not authorize joining. |
| GROUP-04: only owner invitation authority | A regular full member attempts to invite another account. | It cannot grant invitation eligibility using the owner's authority. The group does not become open to arbitrary joining. |
| GROUP-05: pre-join history is not unread | Retain messages and an attachment before B joins. B joins, checks its inbox, and explicitly queries history. | B can query retained pre-join content and fetch its files, but those messages are not added to B's unread queue. New post-join messages are eligible for B's inbox. Joining does not restore content already purged. |
| GROUP-06: leaving revokes access | B leaves while retaining message IDs, attachment IDs, history cursors, and a claim credential. Retry group reads and downloads. | Group access is lost immediately, including history and files. Old cursors or credentials cannot bypass current membership checks. Previously downloaded independent copies are outside service access control. |
| GROUP-07: no kicking feature | Inspect the public HTTP contract, CLI commands, MCP tools, and any member-management UI offered to agents. | First-release interfaces do not implement member-kicking as an added feature. Owner departure/disablement is tested only after that separate lifecycle decision is recorded. |

## Inbox, claims, fencing, ACKs, and history

Requirement categories: REQ-24 through REQ-32.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| INBOX-01: unified account inbox | Give B private messages and messages from two joined groups; include a pending group. Check all/filter-private/filter-groups/filter-specific-channel. | Results cover the requested authorized channels without leaking pending-group messages or another account's private messages. |
| CLAIM-01: check does not reserve | Both B sessions check the same unread summaries, then one session claims directly without another check. | Checks do not reserve messages or change shared unread state. A direct claim succeeds when eligible. |
| CLAIM-02: simultaneous sessions | Synchronize two B sessions to claim the same single eligible message at once. | Only one active claim succeeds. The other cannot receive an independently valid claim for that message during the same validity interval. |
| CLAIM-03: batching and ordering | Insert messages in a known server order. Claim a batch with no ordering argument, then repeat on fresh fixtures with newest-first selected. | Default claims are oldest-first; newest-first reverses the selection order. No message appears twice in a batch, and no already leased message is silently assigned to another session. |
| CLAIM-04: 60-second default and re-claim | Claim at server time t using the default setting. Before t + 60 seconds, attempt another claim; after expiration, try again without an ACK. | The original lease excludes other sessions while valid. The unacknowledged message becomes claimable after expiration. Tests use the configured server deadline, not an arbitrary client clock. |
| CLAIM-05: expired and superseded credential fencing | Session 1 receives credential A. Let it expire; session 2 obtains credential B. Attempt ACK with A, then ACK with B. | A is rejected even if session 1 previously fetched the content. B can ACK while valid. The stale request does not acknowledge, release, or overwrite B's claim. Also reject an expired A before any replacement claim exists. |
| CLAIM-06: credential scope | Use B's claim credential on a different message or under a different account. | The service rejects it and leaves the legitimate claim and unread state unchanged. |
| CLAIM-07: globally changed duration | Change the global lease duration and query settings; issue new claims. | New claims use the configured duration. Handling of claims issued before the change must be explicitly selected in the protocol plan and then tested; discovery did not settle that policy. |
| ACK-01: read without task completion | Claim a message with an attachment and ACK it without downloading the file or executing its text as a task. | ACK succeeds with the current valid credential and records account-wide read state. It does not assert task completion or successful file download. |
| ACK-02: retention is not renewed | Claim/ACK the oldest message, then add enough messages or files to cross a channel limit. | ACK does not extend retention or protect the message/file pair from eviction. |
| ACK-03: acknowledged-token retry deadline, proposed | ACK successfully, retry with the same account and token before its original expiration, then retry after that deadline. | Before expiration, the proposed retry response may report `already_read`. After expiration, the token is rejected even though the message remains read. Expiration does not make an acknowledged message unread again. |
| HISTORY-01: history does not claim or ACK | Read an unread message through history, including its attachment. Inspect both sessions' inboxes and then claim it. | History access neither reserves nor acknowledges the message. It remains unread and claimable unless another session claimed it or retention purged it. |
| HISTORY-02: retained history only | Query read and unread messages that remain retained, then query IDs/cursors for purged messages. | Retained authorized history is available independently of unread state. Purged content is not recovered by a history endpoint. |
| CLAIM-08: local handling is not exactly-once execution | Simulate a client that receives content, performs an external action, and crashes before ACK; allow the lease to expire. | The message may be delivered again. Acceptance is fencing and consistent unread state, not prevention of arbitrary duplicated external effects. |

## Text boundaries, retention, and bidirectional cascade deletion

Requirement categories: REQ-33 through REQ-42 and REQ-47.

For destructive scenarios, inspect all service-managed representations: content rows, unread/claim state, dedup evidence, attachment metadata, managed file paths, staging/recovery work, and content-returning endpoints. An old ACK, history cursor, cached download link, or retried operation cannot restore a deleted record or its old ID. A new send may create a new ID after the comparison record is purged, provided the new request satisfies current limits and authorization. Exact error codes remain protocol choices, but failure must be explicit and must not return the purged content.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| LIMIT-01: Unicode code points | Choose a small body limit. Test ASCII, CJK characters, a combining sequence, a composed emoji sequence, and supplementary-plane characters at the boundary and one code point over it. | Count Unicode code points, not UTF-8 bytes, UTF-16 code units, or displayed grapheme clusters. A valid boundary body is accepted; an oversized body is rejected as a whole with the current limit, with no truncation, splitting, or attachment fallback. |
| LIMIT-02: one attachment and file size | Send a text-only message, text with one permitted file, an oversized file, and more than one file. | Permitted forms succeed. Oversized files and multi-file messages have clear errors and create no incomplete message/file pair. |
| LIMIT-03: message count cascade | Set channel message count N to three. Add a fourth message when the oldest carries a file and is unread; repeat when it has a live claim. | The oldest message, its file, and its unread/claim state are removed in either case. An outstanding credential cannot ACK it back into existence. |
| LIMIT-04: file count reverse cascade | Set file count to one. Retain a text-only message and an older message/file pair, then send a newer message with a file. | The older file and its entire carrying message are removed. Its text must not survive as an attachment-less message. An unrelated retained text-only message is not removed merely to satisfy the file count. Repeat with the old message leased. |
| LIMIT-05: global configuration, independent counters | Populate two private channels and a group to their limits independently. Add data to only one channel. | The global limit values are shared, but each channel has its own counters. Activity or eviction in one channel does not consume another channel's message/file allowance or purge its copies. |
| LIMIT-06: retroactive message/file counts | Lower global retained message count and file count below existing channel counts. Include unread, read, and leased records. | Every affected channel is cleaned to the new limits by evicting oldest relevant records. Both deletion directions apply; unread and leased data have no exemption. |
| LIMIT-07: retroactive body/file size | Lower the code point body limit and single-file byte limit below existing items, including a long body with a small file and a short body with a large file. | An over-limit body removes its message and attachment; an over-limit file removes its attachment and carrying message. Neither text nor detached files survive the cascade. |
| LIMIT-08: no retained service backup | Cause retention eviction and retrospective cleanup, then exercise every supported recovery/history/export path and inspect service-managed discard stores. | Purged content is not retained in a separate service backup, archive, recovery bin, or content log. Operational metadata must not become an alternative copy of discarded body/file bytes. |
| LIMIT-09: cleanup interruption | Inject process termination between metadata updates and file cleanup during eviction, then restart. | The proposed cleanup/recovery design converges without serving purged content, retaining orphan permanent files, or reviving stale claims. If cleanup is asynchronous, its completion condition and observable status must be specified before this case can pass. |

Discard tests concern service-managed online content and retained service backups. They do not assert cryptographic erasure of storage remnants or revocation of bytes another agent already saved. A download already in progress cannot retract bytes previously delivered.

## Latest-content deduplication

Requirement categories: REQ-43 through REQ-46, plus REQ-16 and REQ-17 for multi-target requests. No client-supplied unique message ID is required for these tests.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| DEDUP-01: latest same sender/channel | S sends X to B, then sends the same content/details again to B. | Only one retained message is created in S/B. The second result explicitly identifies deduplication rather than silently creating another unread item. |
| DEDUP-02: no TTL | Keep X retained and its comparison position unchanged. Advance the server clock far beyond 60 seconds and retry X. | It remains a duplicate. Claim expiration, elapsed hours/days, and ACK do not establish a deduplication time window. |
| DEDUP-03: another sender intervenes | S sends X in a group, another member sends Y, then S sends X again. | S's X remains its comparison candidate and the second X is deduplicated. Another member's message does not replace S's candidate. |
| DEDUP-04: own message intervenes | In one channel, S sends X, then Y, then X again. | The last X is accepted because S's latest comparison message was Y. This is not deduplication against every retained historic message. |
| DEDUP-05: channel isolation | S sends X in S/B and then X in S/C or in a group. | Each channel evaluates independently. Content in S/B cannot suppress S/C or group delivery. |
| DEDUP-06: purged candidate | Purge S's comparison message via message count, attachment eviction, body/file-size reduction, and group/private retention fixtures. Restore adequate limits where necessary, then retry the identical request while currently authorized. | Its comparison evidence is gone and the identical content can be accepted with a new ID; the old record is not restored. A detached hash or dedup cache must not preserve suppression after the referenced record is purged. Without restoring sufficient limits, an oversized request must instead be rejected. |
| DEDUP-07: no fallback, proposed | Retain an earlier S:X and later S:Y. Purge only Y, for example by lowering a size limit that Y violates while X remains permitted. Send X again under valid current limits. | Under the proposed no-fallback rule, X is accepted rather than compared against the earlier retained X. This expectation is provisional pending resolution of the REQ-44/REQ-46 interpretation. |
| DEDUP-08: concurrent identical requests | Synchronize two requests with identical content/details from S to the same channel, including a file-bearing variant. | One accepted retained message/file pair exists; the other request is deduplicated. No duplicate unread state, permanent file copy within that target, or race-created comparison candidate remains. |
| DEDUP-09: content/detail distinctions | Repeat with text differing only by whitespace, normalization, or newline encoding; same file bytes under changed filename/media type; and different bytes under the same filename. | Outcomes match the explicitly documented equality proposal. Different attachment bytes cannot be mistaken for equality merely because names or sizes match. No wrapper rewrites content in a way that silently changes these rules. |
| DEDUP-10: partial retry after recipient eviction | Send X to B and C. Evict only B's copy, retain C's copy, then retry X to both. | B can receive a new independent message; C remains deduplicated. Each returned result accurately describes that channel's outcome. |

Dedup comparison and creation must have a single concurrency-safe decision point per sender/channel. Fault tests should interrupt attempts around this point and verify that partial failures neither advance an unsuccessful target's candidate nor corrupt another target's result.

## HTTP, CLI, MCP, pagination, and platform parity

Requirement categories: REQ-13, REQ-14, REQ-25 through REQ-32, REQ-47, REQ-48, and REQ-50.

| Case | Setup and action | Required observation |
| --- | --- | --- |
| INTERFACE-01: active calls only | Leave the service and configured clients idle, then poll through each interface. Inspect their exposed protocol surface. | Clients learn about messages by active calls. The first release does not require or add server push, webhooks, SSE, WebSockets, or agent-session wakeups. |
| INTERFACE-02: common semantics | Repeat send, partial send, check, claim, ACK, history, settings, membership, and file cases through HTTP, CLI, and MCP. | The same account/key/source-IP checks, unread state, lease credentials, ordering, limits, and errors apply. CLI or MCP access must not auto-ACK a check/history read or substitute task completion for read ACK. |
| INTERFACE-03: local file paths | Upload a binary fixture from one host and download through each relevant client on another OS. | A client-side path is read on the caller's host and bytes reach the service. A path string is not treated as a readable path on the server or another recipient. Downloaded bytes and SHA-256 match, within configured limits. |
| PAGE-01: bounded batches/pages, proposed | With configured cap K and more than K eligible results, request K, K + 1, zero, negative, omitted, and malformed sizes. | The documented bounded default/error behavior is consistent across all interfaces. No request returns or claims more than the cap, and wrappers cannot bypass it. Explicit oversized-size rejection is the proposed policy. |
| PAGE-02: stable traversal, proposed | Traverse history/directory pages with equal timestamps, concurrent inserts, and retention deletions, including deletion of a cursor's source record. | Stable cursors and tie breakers avoid duplicate traversal and server errors; authorization and retention are checked on every request. New-data/snapshot behavior must be stated. Purged content is never revived to satisfy a cursor. |
| PAGE-03: batch leases | Claim K messages while another session claims an overlapping selection and retention removes one eligible/leased record. | Each message has at most one current valid account-wide claim. Purged records cannot be returned or ACKed successfully. Batch results remain bounded and each credential refers to its actual message. |
| PLATFORM-01: installation and client matrix | On future Linux, macOS, and Windows test hosts, install the chosen release and exercise HTTP/CLI/MCP as applicable. Record named harness, profile, version, OS, and network route. | Installation and compatibility are demonstrated per actual environment. A document describing Codex, Claude, pi, Qwen, DeepSeek harness, Hermes, or a ChatGPT dot is not a passed integration test. |

## Performance benchmark specification

### Confirmed target and unconfirmed measurements

REQ-49 sets a **100 requests/second peak load-test target for ordinary API operations**: checks, claims, ACKs, directory reads, and ordinary text messaging. It is not a service-side 100 requests/second hard rate limit, is not a measured result, and does not set the file-transfer throughput target. The service must not reject the 101st request merely because the benchmark target is 100 RPS.

Any optional defensive rate limits are separate proposed settings. If introduced, record them and configure them so they do not invalidate the intended benchmark. A rate-limit response is not evidence that the system achieved the throughput target.

All hardware and load results are currently pending:

| Record | Value before execution |
| --- | --- |
| Server model, CPU, allocated cores, memory, OS/kernel, and VM limits | Pending measurement |
| Storage model, filesystem, available space, and database/storage configuration | Pending measurement |
| Client/load-generator hardware, OS, runtime, and generator revision | Pending measurement |
| LAN/Tailscale route, link speed, round-trip latency, and TLS/proxy configuration | Pending measurement |
| Application revision, build flags, dependencies, and effective settings | Pending implementation and measurement |
| Achieved RPS, latency histograms, errors, resource usage, and file throughput | Pending execution; no results claimed |

### Proposed future workload and method

The following is a proposed reproducible method, not an additional confirmed user requirement:

1. Use an open-loop generator with a recorded arrival schedule so slow responses do not silently reduce offered load. Measure end-to-end latency from intended request scheduling through response completion, and report generator saturation separately.
2. Seed retained histories and test both below-cap channels and channels undergoing regular eviction. Use a proposed fixture of 50 logical accounts with 100 sessions so claim contention exercises shared-account behavior.
3. Warm up for 60 seconds. Run a stepped baseline and then a proposed five-minute 100 RPS observation period; record actual offered, accepted, and completed rates. This sustained observation is a proposed way to characterize the confirmed peak target, not a user-approved duration requirement.
4. Use a proposed ordinary-API mix: 30% private text sends, 10% group text sends, 25% checks/claims, 20% ACKs, 10% history reads, and 5% directory/settings reads. Arrange claim and ACK fixtures so expected empty queues, conflicts, or invalid credentials are labeled rather than counted as unexplained failures. Record body-size distribution, batch/page sizes, group membership, and fanout counts.
5. Count an API request once for offered RPS, and separately report messages/target deliveries produced. A fanout request is not falsely reported as several incoming requests. Include deduplicated and partially successful sends as their own result categories.
6. Run file transfer separately with a matrix of file sizes below the configured limit, concurrency levels, upload/download directions, and one-to-many fanout. Report useful bytes/second, file completion time, copy/storage amplification, integrity, and effects on ordinary-API latency. Do not combine bytes/second and ordinary request throughput into one score.
7. Add a proposed mixed-workload run with the same 100 ordinary RPS while two file-transfer streams operate, plus a separate retrospective-cleanup run. Record exact file sizes and rates. Global cap reductions, disk pressure, and cleanup must not break correctness while measurements run.
8. Verify content, claim fencing, deduplication, per-channel counts, file/message cascade deletion, and absence of cross-recipient purges after the run. Throughput alone cannot pass acceptance if messages or authorization rules are wrong.

### Proposed latency and reliability targets

These values have **not been confirmed by the user** and cannot be described as achieved:

| Proposed target | Measurement scope |
| --- | --- |
| p95 at or below 200 ms and p99 at or below 500 ms | Ordinary API responses at offered 100 RPS on the recorded test hardware/network, reported separately per operation and in aggregate; excludes transfer of file bodies and a synchronous administrative cleanup response. |
| Fewer than 0.1% unexpected server/transport failures | Valid ordinary benchmark requests; expected authorization denials, empty claims, dedup results, and intentional stale-token tests are reported separately. |
| No correctness violations | No cross-account content disclosure, simultaneous valid duplicate claims, false ACK success, corrupted file bytes, lost accepted retained messages, or retention/dedup invariant failures. This does not mean arbitrary client tasks execute exactly once. |
| Explicit mixed-workload report | Report latency changes during file transfer and retrospective cleanup. No separate mixed-workload latency ceiling or file-throughput number has yet been selected. |

Report overload behavior above the confirmed target as characterization, not as an invented first-release throughput commitment. Preserve the distinction between benchmark arrival rate, optional rate-limit settings, concurrent requests, completed target deliveries, and bytes transferred.
