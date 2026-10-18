# Changelog

This file records repository changes. Entries describe source changes; a published release and verification evidence are reported separately.

## Unreleased

### Added

- Initial English and Simplified Chinese project descriptions.
- Confirmed requirement records for the planned agent communication service.
- Contribution, conduct, security, issue-reporting, and pull-request guidance.
- MIT license as the maintainer's initial planning default.

- Go HTTP service with SQLite persistence, host/IP and agent-key authentication, private fan-out, groups, polling claims/ACKs, attachments, and retention cleanup.
- Embedded administrator console, shared HTTP client, JSON CLI, and 18 official-SDK stdio MCP tools.
- Concurrency, authorization, persistence, cleanup, real executable, HTTP/CLI/MCP smoke, optional Chromium UI, and local load checks.
- Generated OpenAPI request schemas, pinned modules, complete dependency notices, and native CI definitions.

- Seven-language administration UI with browser detection, saved preference, localized static/dynamic text and validation, and live switching that preserves form/dialog state (REQ-53).
- Administrator API and seven-language channel history inspector for retained private and group messages, with search, type filters, pagination, message ordering, and attachment metadata (REQ-54).
- Agent-facing English and Simplified Chinese quickstarts, HTTP API walkthrough, local stdio MCP setup with Codex examples, and a self-contained installable CLI/MCP skill.
- Embedded administration favicon and an authenticated CPU/GPU/memory/disk overview using native system interfaces without new dependencies (REQ-56, REQ-61).

### Changed

- Administration file-size settings use MB for inputs and change confirmations; new data directories default to a 50 MB single-file limit (REQ-55).
- Administration console redesign: token-based light/dark styling with system fonts, sidebar/icon-rail/bottom-tab layouts, status-oriented overview, filterable and sortable lists with stable name order, row action menus, phone cards and bottom sheets, ID and key copy actions, grouped limits with unit equivalents and a change-diff confirmation, and dialog focus on the first task control.
- Key and ID copy actions fall back when the Clipboard API is unavailable or denied. Private/group channel tabs include icons, participant and owner filters, and retained message-body search; the agent dialog inherits the interface language without its own selector (REQ-57 through REQ-60).

### Verification

See [validation](docs/validation.md) for executed checks and limits. This source publication does not create a binary release or certify every target platform or agent application.
