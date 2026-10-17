# Contributing to Agent Post

Agent Post contains an initial Go service, CLI, local MCP adapter, embedded admin UI, and tests. See [runtime](docs/runtime.md) for the implemented contract and [validation](docs/validation.md) for actual verification. Useful contributions include correctness fixes, reproducible tests, design review, and clearer examples.

## Propose a change

Describe the concrete problem, the proposed behavior, and its effect on existing requirements. Reference stable IDs from [docs/requirements.md](docs/requirements.md). Preserve those IDs when editing or reorganizing the document.

Separate confirmed user requirements from implementation proposals and example defaults. A proposed technology, packaging format, or configuration value must not become a confirmed product decision solely because it appears in an example.

Use an issue to describe a problem or discuss an alternative. A focused pull request can include the proposed change directly. Keep its scope small enough to review, and update related documents when changing a protocol or architecture decision.

## Prepare a pull request

Explain what changes for a reader or future implementer, why the change is needed, which requirement IDs it affects, and how you checked it. State any assumptions or unresolved decisions.

Run `go test -race ./...`, `go vet ./...`, `python3 scripts/check_docs.py`, and `python3 scripts/smoke.py` for relevant application changes. The smoke script builds and runs an isolated real service using temporary synthetic fixtures. Optional `python3 scripts/smoke.py --benchmark-seconds 60 --file-benchmark-rounds 10` characterizes local load; report its exact workload and scope. Use Chromium for `node scripts/browser-smoke.cjs` and `node scripts/browser-i18n-smoke.cjs` UI checks (requires Playwright; CI pins Playwright 1.62.1). The i18n suite exercises all seven languages, real localized operations, preference detection/persistence, mobile containment, and live-switch state preservation. Keep catalog keys and interpolation placeholders complete for every locale. Verify document links, examples, terminology, and consistency with confirmed requirements. Regenerate the shared request contract with `go run ./cmd/contract > docs/openapi.json`. Regenerate third-party notices when dependencies change. Report checks actually performed; a workflow definition is not evidence of a passing run.

The public [implementation plan](docs/implementation-plan.md) explains the design and implementation sequence. Do not turn it, the README, or another document into a duplicate progress checklist. Maintainer task status is recorded separately in the maintainer development log.

## Protect private information

Use fictional identifiers and placeholders in examples. Do not submit API keys, credentials, personal file paths, actual private host addresses, or confidential agent conversations. For security-sensitive concerns, follow [SECURITY.md](SECURITY.md) instead of posting exploit details publicly.

Agent-generated contributions should receive the same factual review as any other contribution. Verify claims and sources, and never report a tool run, benchmark, or compatibility test that did not happen.

Participation follows the [Code of Conduct](CODE_OF_CONDUCT.md). Contributions are made under the repository's [MIT license](LICENSE), which is the maintainer's initial planning choice.
