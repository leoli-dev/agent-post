# Validation evidence

Initial implementation checks executed on **2026-10-17**, against clean source revision
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

## Source publication and hosted CI

The source repository is public at [leoli-dev/agent-post](https://github.com/leoli-dev/agent-post),
with `main` as its default branch. GitHub private vulnerability reporting is
enabled. No versioned binary Release was created.

The first hosted run passed Linux and macOS native build/vet/race jobs and
the generated-contract/real-service smoke job. Its Windows application tests
passed except the executable-subprocess test, which lacked a `.exe` suffix.
Test-only commit `f4847fb2b0b4dca7eb057584da2e41e04f142b73` corrected the native
executable name, passed local focused race tests, and received independent review.
The application runtime is unchanged from the measured revision.

[The corrected application run](https://github.com/leoli-dev/agent-post/actions/runs/39072955736)
completed successfully at `f4847fb`: all three native build/vet/race jobs
(`ubuntu-latest`, `macos-latest`, `windows-latest`) and the Linux generated-contract
plus real-service smoke job passed. [Documentation CI](https://github.com/leoli-dev/agent-post/actions/runs/39072955650)
also passed. These are actual hosted native test results; they do not certify
every OS/architecture pair, named agent application, installer, or power-loss
scenario in the release acceptance specification.

## Seven-language administration follow-up

REQ-53 adds English, French, Simplified Chinese, Traditional Chinese, Italian,
Spanish, and Russian. Final local checks on `d048f02` include administration
implementation commits `48510a4`, `4ec22d2`, and `8807d22`; the agent protocol and
backend were unchanged. No new throughput measurement was performed for this
UI-only change.

- `go test -race ./...`, `go vet ./...`, the real-service HTTP/CLI/MCP smoke,
  JavaScript syntax checks, documentation checks through REQ-53, and diff checks
  passed. The four Node i18n tests passed: complete 173-key catalogs and matching
  placeholders in all seven locales, coverage of static attributes/settings,
  browser/stored-language resolution, and live text/accessibility changes without
  changing input values.
- The existing English Chromium smoke passed. The new
  `scripts/browser-i18n-smoke.cjs` passed in Chromium 155.0.8051.0, with zero page
  JavaScript errors. Every locale exercised translated login and native
  required-field feedback, authentication errors, host validation/creation, agent
  and key creation, key preservation while switching in a dialog, unsaved limits
  and pending-confirmation preservation, actual limit saves, session reload,
  mobile table containment, and logout preference persistence.
- Twelve fresh browser-language cases passed, including regional French/Italian/
  Spanish/Russian, Simplified/Traditional Chinese regions, conflicting Chinese
  script/region tags, and unsupported-language fallback. Explicit saved preference
  and invalid-preference fallback passed. Switching with blocked browser storage
  also passed. A user-provided host name equal to a translation key remained
  verbatim; markup-like names remained inert text.
- Screenshots at width 390 were inspected, including Russian and Traditional
  Chinese. Dense tables scroll inside their panels, with usable name/ID widths.
  Dialogs reference their translated titles and confirmation warnings through ARIA.
- The high-effort independent reviewer reported no remaining actionable findings.
  A separate wording review covered French, Italian, and Spanish; corrections to
  claim terminology and limit wording were applied. This was agent-assisted
  translation review, rather than a claim of professional native-speaker sign-off.

The application workflow now defines a dedicated Chromium job using pinned
Playwright 1.62.1 and Node 22, running the Node suite and both browser scripts.
[The hosted application run](https://github.com/leoli-dev/agent-post/actions/runs/39073953836)
completed successfully at `003da8e`: the dedicated Chromium job passed all four
Node tests, the English smoke and the complete seven-locale browser suite.
Linux, macOS, and Windows native build/vet/race steps, the generated contract,
and the real-service smoke also passed. [Documentation CI](https://github.com/leoli-dev/agent-post/actions/runs/39073953840)
passed for the same revision.

## Administration console redesign

The console was redesigned in four commits (`8881ca3`, `5697c4f`, `6f6e1fe`,
`1995680`) without backend or protocol changes. Local checks on the final source
used Playwright 1.62.1 with its bundled Chromium 151.0.7922.34; no new throughput
measurement was performed.

- `go test -race ./...`, `go vet ./...`, `node --check` for the console and test
  scripts, the documentation check through REQ-53, the real-service HTTP/CLI/MCP
  smoke, and `git diff --check` passed. The four Node i18n tests passed with
  200-key catalogs in all seven locales.
- The English Chromium smoke passed with zero page errors. Beyond the earlier
  flows it now covers stable natural name order across refreshes, name sorting
  and `aria-sort`, text, IP and status filters, dialog initial focus, issued-key
  and clipboard copy, keyboard row menus (Enter, arrows, Escape returning
  focus), destructive styling and target names in confirmations, the limits
  change list, Discard, non-destructive versus lowered-retention confirmations,
  the overview attention list, phone cards without horizontal scrolling,
  bottom-sheet row actions, and sign-out through the phone menu.
- The seven-locale Chromium suite passed unchanged, including language switching
  inside dialogs from the relocated footer picker.
- Screenshots were inspected at 1440, 1180, 820, 390, and 360 pixels wide in
  English, Russian, French, and Traditional Chinese, in light and dark schemes,
  with no page-level horizontal overflow. Computed contrast for every text and
  status token on its backgrounds is at least 4.74:1 (light) and 5.71:1 (dark).
- New and changed strings were translated with agent assistance; there was no
  native-speaker review for this change.

[The hosted application run](https://github.com/leoli-dev/agent-post/actions/runs/39078068746)
completed successfully at `011642d`: the admin UI job (Node tests, English
smoke, and the seven-locale Chromium suite), Linux, macOS, and Windows native
build/vet/race, and the contract smoke all passed. [Documentation CI](https://github.com/leoli-dev/agent-post/actions/runs/39078068735)
passed for the same revision.

## Administrator channel history follow-up

Local checks on **2026-10-17** exercised REQ-54's administrator history API and
console against retained private and group channels. These checks used synthetic
fixtures and temporary service data; no new throughput measurement or hosted CI
run is reported for this change.

- `go vet ./...`, documentation checks through REQ-54, JavaScript syntax checks,
  and `git diff --check` passed. Every Go package passed race tests with
  `-count=1`, across separate server and remaining-package runs; the server run
  took 125.049 seconds. All four Node i18n tests passed with complete 226-key
  catalogs and matching placeholders in all seven locales.
- Focused backend tests passed administrator cookie and Bearer authentication,
  expired-session and agent-key rejection, history for disabled identities,
  retained message/file counts, live cursor traversal across retention gaps,
  and coupled message/attachment deletion.
- The existing English and seven-language Chromium suites passed in Chromium
  151.0.7922.34 at desktop width 1440 and mobile width 390, with zero page
  JavaScript errors.
- `scripts/browser-history-smoke.cjs` passed against a real service in the same
  Chromium version. It covered empty channels and histories, private and group
  messages, channel type and name/ID search, channel and message pagination,
  oldest/newest ordering, literal rendering of message bodies, filenames and
  names, attachment metadata, all seven languages, mobile containment, and
  administrator cookie-session restoration. A delayed response for a previously
  selected channel did not overwrite the new selection. The Groups view's
  shortcut opened the correct channel. Inspection issued only GET requests and
  preserved unread counts, claim availability, leased state, and the current
  claim token. Populated desktop and mobile screenshots were visually inspected.

Translations were agent-assisted without native-speaker review. These local
checks do not establish new platform, integration, performance, or release
claims.

## Administration file-size settings follow-up

Local checks on **2026-10-17** exercised REQ-55's MB settings and the 50 MB
single-file default. These checks do not replace the historical benchmark's
25 MiB setting or establish new platform, performance, or release claims.

- `go test ./internal/adminui`, `go vet ./...`, documentation checks through
  REQ-55, and `git diff --check` passed. All four Node i18n tests passed.
  Focused server tests passed for current upload limits, attachment cleanup and
  restart, and strict newest-window retention before file eviction.
- The English and seven-locale browser suites passed in Chromium
  151.0.7922.34 with zero page JavaScript errors. Checks covered the 50 MB
  default; saving 12.5 MB as 12,500,000 API bytes and reloading it; MB units in
  previous-value hints and confirmations in all seven locales; and preservation
  of 12,345,678 saved bytes as 12.345678 MB during an unrelated settings save.
  Fractional bytes were rejected, one-byte and upper-bound values remained
  exact, and the temporary upload budget still had to accommodate one file.
- An independent conversion check passed 30,000 integer-byte round trips through
  the MB helpers and rejected half-byte values near the lower and upper bounds.

The HTTP settings contract and stored values remain integer bytes. Translations
were agent-assisted without native-speaker review.

## Agent onboarding documentation and skill

Local checks on **2026-10-17** exercised the agent quickstart, HTTP examples,
and bundled instruction-only skill on macOS arm64 with Go 1.26.3. Checks used
temporary installations, synthetic accounts, and isolated service data.

- A source `go install` into a temporary `GOBIN`, protected explicit JSON config,
  identity/limits/directory queries, private send, inbox, claim, ACK, history,
  reply, and copying the self-contained skill folder all passed.
- Real curl requests passed authentication, cursor traversal, partial fan-out,
  duplicate suppression, exclusive claims, per-item ACK statuses, group
  invitation/join/history/leave, multipart attachments, authorized download,
  and body/JSON/source-IP rejection checks.
- `python3 scripts/smoke.py` passed real HTTP, CLI, and stdio MCP flows. Focused
  MCP/client Go tests, skill frontmatter validation, documentation checks, and
  whitespace checks passed. Independent source review corrected settings field
  names in the quickstart and checked tool names and response examples.

Before committing the combined history, MB settings, and onboarding changes,
`go test -race -count=1 ./...`, `go vet ./...`, all four Node i18n tests, and the
English, seven-language, and channel-history browser suites passed. These browser
checks explicitly used Chromium 155.0.8051.0 and reported zero page errors.
Go formatting and the generated OpenAPI request contract matched. Independent
review found no blocking correctness or security issues in the administration
changes. No new throughput benchmark was run.

These checks verify local instructions and transports. They do not certify
skill discovery or end-to-end use in a named third-party agent application,
establish a new platform support claim, or measure throughput.

## Administration usability and resource overview follow-up

Local checks on **2026-10-18** exercised REQ-56 through REQ-61 with synthetic
accounts and temporary service data on macOS arm64.

- `go test -race -count=1 ./...` passed; the server package took 173.636 seconds.
  Subsequent focused metric race tests also passed after isolating shared probe
  cancellation from individual HTTP clients. `go vet ./...`, Go formatting,
  JavaScript syntax checks, documentation checks through REQ-61, and
  `git diff --check` passed. All four Node i18n tests passed with complete
  catalogs and matching interpolation parameters across seven locales.
- Focused channel tests cover body-only search, combined exact participant/owner
  filters, active versus pending/left members, filtering before pagination,
  retention gaps, and unchanged claims/read state. Names, IDs, and attachment
  filenames do not match message-body search. Ordinary agent history retains
  its existing behavior.
- The English, seven-language, and channel-history browser suites passed in
  **Chromium 155.0.8051.0**, with zero page errors. They cover the embedded
  favicon, private/group tabs and icons, content search in channel and message
  lists, filter combinations, paging, group shortcuts, literal rendering of
  untrusted content, session reload, and mobile containment. The agent form has
  no language selector. Clipboard checks read the actual system clipboard and
  compare it to the issued key, including missing and denied Clipboard API
  fallbacks. A delayed rejection after the key dialog closes preserves the
  clipboard and does not falsely report success.
- Metric tests cover administrator cookie/Bearer authentication, agent-key
  rejection, command deadlines/output limits, cache sharing, canceled callers,
  native-output parsers, and sampling outside the messaging state mutex.
  A native macOS smoke returned CPU, GPU, memory, and disk counters from
  `top`, `ioreg`, `sysctl`/`vm_stat`, and `df`; repeated immediate reads reused
  the sample timestamp. Linux and Windows parser tests use fixtures; their new
  collectors were **not executed natively** in this follow-up. GPU counters
  depend on the system/driver, and the Windows implementation reports GPU
  utilization unavailable. Disk reports occupied filesystem capacity, and
  macOS memory includes cached/inactive pages.

No new dependencies, published release, hosted CI result, or throughput benchmark
is reported for these changes. New translation strings have not had native-speaker
review.

## README installation and scheduled-agent documentation review

Local checks on **2026-10-18**, on macOS arm64 with Go 1.26.3, reviewed the
English/Chinese README paths against the server, CLI, and MCP implementation.
The changes add installation guidance and clarify client/scheduler policy;
they do not add a scheduler, key scopes, or a sandbox to Agent Post.

- A source installation into a temporary `GOBIN` passed. The bilingual server
  startup blocks matched and passed shell syntax checks. A temporary-terminal
  check verified that hidden password input disabled echo without printing the
  secret. Isolated HTTP checks passed health/admin access, administrator login,
  host/account creation, protected-config CLI identity/limits/directory access,
  wrong-key and source-IP rejection, and forwarded-header bypass rejection.
  Graceful shutdown, rejection of a changed administrator password, and restart
  with the original password/data directory preserved the account.
- `python3 scripts/smoke.py` passed real HTTP, CLI, and stdio MCP flows with
  synthetic accounts and temporary storage. Focused tests passed with
  `go test ./internal/client ./internal/cli ./internal/mcp ./cmd/agent-post`.
- The protected-header helper and actual curl requests passed against a local
  HTTP fixture. The check verified the expected Authorization header without
  placing the key in curl's argv, mode 600 output, overwrite refusal, and
  rejection of insecure source-file permissions or whitespace in a key.
- Documentation links, local heading anchors, shell syntax, JSON/TOML snippets,
  bilingual polling/profile template consistency, and `git diff --check` passed.
  A temporary Codex stub verified the proposed entrypoint's prompt stdin,
  arguments, exit-status propagation, output suppression, and invalid-input
  rejection. This establishes wrapper behavior, not model reading or delivery.

Codex CLI flags were checked against installed `codex-cli 0.162.1` help and
official documentation. No model turn, Hermes deployment, Linux cron service,
scheduled reply, or live third-party integration was executed in this review.
Those require commissioning from the actual scheduled context as described in
the [operating manual](agent-operations.md#6-commission-and-stop-the-integration).
No new platform-support, security-audit, release, or benchmark claim is made.
