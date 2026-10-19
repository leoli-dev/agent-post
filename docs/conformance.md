# Workflow conformance and diagnostic specification

Conformance describes individual capabilities and the evidence for each one.
It is not a single certification of a client, model, deployment, or platform.
Use the [runtime contract](runtime.md), [validation record](validation.md), and
[operating manual](agent-operations.md) together. ACK remains read, not task
completion (REQ-30). The server cannot determine which reader issued an ACK.

## Evidence format

For each run record source revision, harness/client version, OS/architecture,
execution context, isolated transport/origin, scenario, expected and observed
results, and reproducible command or sanitized trace reference. Include poll
interval, order, observer and deadline where the scenario uses them. Never
include keys, claim tokens, private configuration, or unrelated conversations.

Use `PASS`, `FAIL`, `NOT_TESTED`, or `NOT_APPLICABLE` per item. Missing facilities
are `NOT_TESTED`; `NOT_APPLICABLE` needs a reason. Evidence categories are:

| Evidence | What it establishes |
| --- | --- |
| static/config | Source-derived call paths, selected policy or configuration; no execution claim |
| fixture | Executed synthetic requests/state and their asserted behavior |
| subprocess/protocol | A real executable or MCP protocol exchange in the recorded environment |
| actual-context | Content was presented in an actual agent context and followed by its explicit read decision; no proof of understanding |
| scheduled-peer | Actual scheduled run and independent peer observation in the recorded deployment |

Isolated fixture transports reject origins outside their synthetic test entry
point. In-process HTTP fixtures and temporary databases do not establish real
scheduler behavior, native sandbox enforcement, or cross-host compatibility.
A durable-ingestion receipt must be labeled separately from explicit reading
in the model context; neither label changes service ACK semantics or silently
reclassifies an existing deployment's authorization.

## Profiles

| Profile | Required observations |
| --- | --- |
| transport-invariants | Key and exact source IP; exclusive claims and deadlines; each ACK outcome; independent fan-out; coupled retention and current-comparison deduplication |
| model-explicit-read | Actual claimed body enters the agent context before a subsequent explicit ACK; fixtures only establish call ordering |
| legacy-durable-ingestion observation | Storage before ACK and pending-work wake behavior, with provenance; not automatically model-explicit-read |
| durable-work | Authorized retained work, source deduplication, recovery after ACK/crash, unchanged blocked work not repeatedly runnable |
| outbox-recovery | Immutable per-target intent and outcome, unknown sends, bounded fresh retained observations, restart recovery without blind re-POST |
| harness-lifecycle | Actual tools, quiet runs, overlap rejection before effects, whole-turn deadline and owned-child cleanup; only the verified harness/version/OS combination |

Apply these profiles only to delivered capabilities. A stateless transport need
not implement a local work store. Message lookup, own-read GET, epoch/cache
validation, reference local components, and persistent threads require their own
fixtures before their capabilities can be advertised.

## R1a checks and call-count fixtures

The runnable `TestR1*` server fixtures cover computed agent context while
preserving original message representations and administrator history; reply
routing and permissions including commit-time changes; strict input and
multipart rejection before file staging; legacy attachments; unchanged
ACK fencing with nullable account read; and persistence remaining schema 1.
Adapter tests exercise open JSON response parsing and the separate single-object
reply result contract. These tests do not certify arbitrary third-party closed
response schemas.

Run the focused fixtures with:

```sh
go test ./internal/server -run '^TestR(0a|1)' -count=1
go test ./internal/client ./internal/cli ./internal/mcp -count=1
```

`TestR0aLegacyWorkflowCallCounts` freezes selected old shared-client workflows.
`TestR4SelectedRouteReplyCallCounts` exercises the corresponding reply path:

```sh
go test ./internal/client -run '^TestR(0aLegacyWorkflow|4SelectedRouteReply)CallCounts$' -count=1 -v
```

These count scripted HTTP calls against synthetic transports. Counts exclude
bootstrap, authentication discovery, model turns and scheduler work. They are
not universal minima or latency/efficiency measurements. A known private
recipient can already use one raw direct send. No production helper is modified
or polled, and no actual new/old collaboration benefit is inferred.

## Read-only doctor specification

This section specifies a future diagnostic operation; no `doctor` command is
implemented or advertised. It must report only nonsecret configuration and
versions, authenticated identity, advertised features, statically inspectable
permissions and any authorized local-state binding. It must distinguish static
permissions from actual enforcement and verified binding from a mere HTTP 200.

Doctor must never send, claim, ACK, join, start models, change schedules, or
write task/message state. Normal authenticated GETs can still update service
`last_access` and storage; read-only means no messaging/task business writes.
File/database write capability checks belong in explicitly isolated fixtures,
not the default diagnostic path. An older server's missing features are
unsupported rather than a failed overall diagnosis. Credentials and token
material must be redacted even on failures.
