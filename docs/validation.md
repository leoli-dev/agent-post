# Validation evidence

Executed on **2026-10-17**, against clean source revision
`a72ba01c8526d46b430519a92ce9953bc2e38703`. This report describes initial source
verification. [Acceptance](acceptance.md) remains the broader specification,
not a report that every scenario passed.

## Environment and method

Local service and load generator ran on the same macOS arm64 host: Darwin
27.0.0, Apple M5 Max, 18 logical CPUs, 128 GiB RAM, local APFS SSD storage.
Source builds used Go 1.26.3. The test traffic used loopback HTTP without a proxy
or TLS. Disk model, network capacity, power-loss behavior, and system resource
usage were not characterized. No other agent's tests were running during the
60-second measurement.

SQLite used WAL, `synchronous=FULL`, `secure_delete=ON`, and a 5,000 ms busy
timeout. These are configured settings, rather than a power-loss guarantee.
Synthetic fixtures, random credentials, temporary databases, and binary files
were created outside the source tree and removed by the smoke scripts. Raw
runtime outputs and private credentials are not committed.

## Executed checks

- `go test -race -count=1 ./...` passed across all packages. The server suite
  includes 18 tests and an example; the final server run took 72.599 seconds.
  The independent reviewer also ran the complete race suite successfully.
- `go vet ./...`, `go mod verify`, and a trimmed executable build passed.
- `python3 scripts/check_docs.py` passed document links, formatting, public-path
  checks, and REQ-01 through REQ-52 presence. Generated OpenAPI matched
  `go run ./cmd/contract` byte for byte. All 71 embedded request JSON Schemas
  resolved with the same schema engine used by the MCP SDK; this was not a full
  OpenAPI conformance audit.
- `python3 scripts/smoke.py --benchmark-seconds 60 --file-benchmark-rounds 10`
  passed against a real built service and real CLI/MCP subprocesses. It checked
  key plus exact source IP admission, forwarded-header spoof denial, admin-role
  isolation, partial fan-out, deduplication, channel isolation, exclusive claims,
  expired/replaced tokens, group invitation/join/history/leave behavior,
  independently owned physical file copies, authorized download, isolated file
  eviction, both deletion directions, retroactive limits, Unicode code point
  boundaries, explicit CLI ACK, official SDK stdio negotiation/tool invocation,
  and durable history/read state/settings after graceful restart.
- `node scripts/browser-smoke.cjs` passed in actual **Chromium 155.0.8051.0**
  at desktop width 1,440 and mobile width 390. It operated login, host
  registration, account creation, key rotation with old-key rejection,
  disable/enable, limit updates, cookie-session restoration, mobile containment,
  and logout, with zero page JavaScript errors.
- `gpt-6.1-sol` at high reasoning effort independently reviewed the complete
  implementation after the medium-effort coder subagents. Its final review at
  the revision above reported no actionable findings. Earlier locking,
  persistence rollback, Unicode, pagination, retention ordering, and file-cleanup
  findings were fixed and covered by regressions.

## Local HTTP characterization

The open-loop generator offered **100 requests/second for 60 seconds** using 32
workers and a fresh HTTP connection per call. One synthetic sender/receiver
pair exchanged unique short text. Each operation below accounted for 20% of
offered load; 100 warmup messages and claims seeded the ACK pipeline.

For this run, message retention was raised to 10,000 per channel and claim
validity to 300 seconds so the measurement exercised increasing retained state
and valid ACKs. Other limits used initial defaults: 8,192 body code points,
25 MiB files, 10 files/channel, claim batch 50, page cap 100, 16 private targets,
64 group members, 32 owned groups, four concurrent uploads, and 100 MiB temporary
upload admission. Claim and ACK batches in the workload were one, and directory
pages requested 100. There was no concurrent file traffic.

| Operation | Completed | Errors | Scheduled p95 | Scheduled p99 |
| --- | ---: | ---: | ---: | ---: |
| Private text send | 1,200 | 0 | 7.277 ms | 7.550 ms |
| Directory | 1,200 | 0 | 7.167 ms | 7.559 ms |
| Inbox check | 1,200 | 0 | 7.325 ms | 7.638 ms |
| Claim | 1,200 | 0 | 7.255 ms | 7.657 ms |
| Valid ACK | 1,200 | 0 | 7.238 ms | 7.689 ms |

All **6,000** offered calls completed within the observation window, with zero
unexpected failures, empty claims, or ACK-queue fallback calls. Achieved
completion rate within the window was 100.0 RPS. Aggregate scheduled end-to-end
latency was **p50 5.737 ms, p95 7.253 ms, p99 7.634 ms**. This includes scheduler
lateness, worker queueing, transport, service execution, and response reading.
Measured request execution alone was p50 3.755 ms, p95 5.208 ms, p99 5.441 ms.

This demonstrates the recorded local workload at the confirmed target arrival
rate. It does not establish the specification's larger five-minute,
50-account/100-session workload, group/history mix, regular eviction, concurrent
file transfers, resource usage, LAN/Tailscale routes, or capacity guarantees.
The server has no 100 RPS hard rate limit.

## Separate file characterization

After the HTTP run, ten sequential 256 KiB uploads and downloads used 64 KiB
stream chunks. Every round verified byte count and SHA-256. Useful upload
throughput was **28,209,050 bytes/second**, and useful download throughput was
**300,560,726 bytes/second**. These aggregate transfer-phase figures exclude
history lookup; combined useful throughput including lookups was 47,338,599
bytes/second. Small loopback samples with warm caches are not network or disk
capacity claims. Concurrent transfers and load interference were not measured.

## Verification boundaries

Native checks above cover this macOS arm64 machine. CI definitions request
native Linux, macOS, and Windows builds/tests; a workflow definition is not a
passing run. Source publication is separate from a versioned binary release.
No named third-party agent application's integration, all six OS/architecture
combinations, installer, upgrade path, destructive process-crash/power-loss
behavior, or Windows open-file durability is certified by these local results.

Storage failure tests inject database write failures and failed filesystem
cleanup, then verify rollback/recovery; they do not substitute for physical
power-loss testing. Current implementation limits include an in-memory working
set, O(retained-state) scans/serialization, no total disk quota, no administrator
password-rotation operation, and no owner transfer or group dissolution. See
[runtime](runtime.md) for the operational contract.
