# Proposed distribution and release policy

This document preserves the proposed release policy. The initial source
implementation can now be built as described in [runtime](runtime.md);
[validation](validation.md) records actual tests. Platform support and binary
release artifacts below remain targets until separately verified. See
[requirements](requirements.md), [architecture](architecture.md),
[implementation plan](implementation-plan.md), and [acceptance](acceptance.md).

## Artifact shape

The proposed release is one platform-specific executable containing the central
service, CLI, local stdio MCP adapter, and administration UI assets. The same
executable is used on a server or a client; the selected command determines its
role. The central service creates its database, configuration state, staging
area, and owned attachment files in an explicitly selected local data directory.

The intended runtime does not require Node.js, Python, an external SQLite
library, or a separately managed database service. No required UI asset should
come from a CDN. Source builds require a Go toolchain and the pinned Go module
dependencies; a low runtime dependency count must not be advertised as having no
source or third-party dependencies.

An initial release does not include a remote MCP endpoint, watch mode, push
delivery, or long polling. The local `mcp` command speaks stdio to its parent MCP
client and ordinary HTTP to the configured central service. Its stdout must
contain only MCP protocol messages; diagnostics belong on stderr. CLI output and
exit status must follow the [protocol](protocol.md), including explicit partial
fan-out outcomes.

## Candidate toolchain and dependencies

These are research records as of 2026-10-17, historical candidate observations. Actual dependency pins are in `go.mod` and
`go.sum`; the implementation currently builds with Go 1.26.3. CI definitions are
present, but a workflow file alone is not a passing run.

| Candidate | Observed version or baseline | Consequence for the proposal |
| --- | --- | --- |
| Go | Minimum source-build candidate: 1.26; release-toolchain candidate: a maintained 1.27 patch release. | The proposed current SQLite driver requires at least Go 1.26. A Go 1.27 build raises the macOS runtime floor to 13. |
| Official MCP Go SDK | `github.com/modelcontextprotocol/go-sdk` v1.8.0; that tag's `go.mod` declares Go 1.25.0. | Use the stdio server for the local adapter. Pin a release and test against the actual host applications. |
| Pure-Go SQLite driver | `modernc.org/sqlite` v1.60.1; its module declares Go 1.26.0 and `modernc.org/libc` v1.77.1. | Keep the driver's expected libc dependency aligned; do not independently select an arbitrary libc version. |
| Embedded SQLite | The v1.60.1 package's supported-platform table records SQLite 3.53.4. | Record and verify the embedded runtime version in the implementation and release evidence. |

The SDK's current stable API and protocol support are useful selection evidence,
not proof that each named agent application supports every MCP feature. Local
stdio compatibility must be verified with the clients the release claims to
support. Message durability and retries must remain in the application's
[protocol](protocol.md), independent of MCP connection state.

SQLite's official WAL documentation records a WAL-reset fix in SQLite 3.51.3 and
later, with specified backports. The selected driver must include a fixed SQLite
version. The proposed candidate is later than that fix, but a future pin still
requires checking the actual embedded version and upstream advisories.

## Planned platform matrix

All six combinations below are planned targets. None is a claim of a successful
build, completed CI, or working installation. A target becomes supported only
after a real artifact passes its required smoke tests on that operating system
and architecture.

| Operating system | Architecture | Proposed release baseline |
| --- | --- | --- |
| Linux | amd64 | 64-bit x86 baseline; local disk for service data. |
| Linux | arm64 | 64-bit ARM baseline; local disk for service data. |
| macOS | amd64 | macOS 13 or later when built with Go 1.27. |
| macOS | arm64 | macOS 13 or later when built with Go 1.27. |
| Windows | amd64 | Windows 10 or later, or Windows Server 2016 or later. |
| Windows | arm64 | Candidate native ARM64 artifact; requires independent native validation. |

The Go project documents a Linux kernel floor of 3.2 for Go 1.24 and later.
Architecture and dependency constraints may add requirements; that toolchain
floor alone is not a project support promise. Proposed x86 releases keep the
default `GOAMD64=v1` baseline rather than requiring newer CPU instructions.

A cgo-free build using `CGO_ENABLED=0` is the proposed route. The claim to verify
is that the resulting artifact does not require an externally installed SQLite
library or a language runtime. It is not a claim that the operating system's own
libraries, APIs, certificate store, filesystem, and network services disappear.

If macOS 12 is required, Go 1.26 is a possible maintained build route while it is
supported upstream. That would be a separately stated build choice with its own
dependency compatibility and smoke-test evidence. No installer, package-manager
formula, download URL, service unit, or installation command is promised by this
plan.

## Release evidence and acceptance principles

A release must identify its source revision, Go toolchain, module versions,
embedded SQLite version, build settings, and platform. Artifacts should carry a
version identifier and be distributed with checksums and required license
notices. Dependency provenance and a machine-readable software bill of materials
are proposed release metadata. Actual publication locations and signing methods
are implementation decisions, not existing facilities.

Cross-compilation establishes that a target can be built; it does not establish
that it runs correctly. Smoke evidence for each claimed target must exercise the
central service's local storage, embedded UI, HTTP authorization, CLI operations,
and stdio MCP connection. A client-only test is insufficient evidence for service
support on that platform. Tests must also verify that agent credentials and exact
host-IP policies have the documented effect, including rejection of spoofed
forwarding headers.

Storage acceptance must demonstrate restart recovery and the contract that every
successful DM fan-out target owns an independent message and physical attachment
copy. A group message owns one copy per file in its group channel, rather than
one copy per member. Deleting one channel must leave another channel's copy
intact. Temporary staging and partial materialization must recover without
reporting incomplete files as accepted messages. A purge must revoke logical
access immediately, retain only opaque paths in recoverable deletion jobs, expose
failed physical cleanup, and block new attachment writes while that failure is
unresolved. Physical deletion may be reported complete only after the files have
actually been removed. Server-side duplicate detection must follow retained
messages and must not depend on caller-generated unique identifiers. The initial
service must not keep automatic backups or recovery archives of discarded
content. Upgrade recovery must preserve these retention and ownership rules.

The 100 requests/second figure is an ordinary-API load-test target, not a rate cap
or a measured capacity. File-transfer throughput must be measured separately.
Release performance evidence must state hardware, OS, file storage, durability
settings, request mix, text sizes, fan-out width, and any competing attachment
activity. It must report errors and latency distributions, and must include
checkpoint and recovery behavior. No unrun benchmark or synthetic estimate may
be presented as a passed acceptance result.

Upgrades must define the database and configuration migration policy before a
release changes those formats. Downgrade support must be stated explicitly;
replacing the executable alone cannot be assumed to reverse a schema migration.
Migration procedures must not quietly introduce retained content backups that
conflict with discard semantics. Full operational acceptance details belong in
[acceptance](acceptance.md).

## License and notices

The source repository uses the MIT license. This is an implementation choice
within the original plan, rather than a requirement chosen during discovery.
Dependencies retain their own licenses; see [third-party notices](../THIRD_PARTY_NOTICES.md).

Upstream licenses must be retained independently of that choice. The official MCP
SDK is undergoing a licensing transition: new code contributions are Apache-2.0,
while original MIT contributions without relicensing consent remain MIT. It must
not be summarized as purely MIT. The current upstream license also identifies
CC-BY-4.0 for non-specification documentation; copying upstream documentation
requires preserving its applicable attribution.

The proposed modernc SQLite package is BSD-3-Clause and includes SQLite, which is
public domain, as well as other code with its own terms. Its package documentation
identifies third-party license and SBOM records. Releases must preserve notices
for the actual linked dependency graph rather than copying an incomplete license
label from a top-level README. The Go distribution's applicable license notices
must also be retained. Candidate versions are not a completed license inventory.

## Why Go is proposed

Go's standard HTTP server, templates, and compile-time asset embedding permit a
small application stack. A pure-Go SQLite driver keeps the proposed build free
of a C compiler requirement for SQLite. This is a practical fit for distributing
one executable to the user's Linux, macOS, and Windows hosts.

Rust can implement the same deployment model and is a viable alternative. A
common SQLite route, `rusqlite` with its `bundled` feature, compiles and links
SQLite into the artifact, avoiding a separate SQLite installation at runtime.
Its build uses the `cc` crate and therefore needs the appropriate C toolchain for
each target. That adds release-build setup relative to the proposed pure-Go
route. This is a deployment-maintenance judgment, not a claim that Go is faster,
smaller, or more reliable; this project has measured none of those comparisons.

## Primary sources

- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and its
  [v1.8.0 module](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/go.mod)
  describe the SDK and its source-build baseline. The
  [v1.8.0 license](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/LICENSE)
  records the licensing transition.
- [modernc SQLite v1.60.1 package documentation](https://pkg.go.dev/modernc.org/sqlite@v1.60.1)
  records the driver and platform information; its
  [tagged module](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/go.mod) records the
  Go and libc requirements.
- [Go minimum requirements](https://go.dev/wiki/MinimumRequirements) and
  [Go 1.27 release notes](https://go.dev/doc/go1.27) describe the operating-system
  and build constraints.
- [Go `embed`](https://pkg.go.dev/embed) describes compile-time asset embedding.
- [SQLite WAL documentation](https://www.sqlite.org/wal.html) describes storage,
  concurrency, durability, and the WAL-reset fix.
- [rusqlite build documentation](https://github.com/rusqlite/rusqlite#notes-on-building-rusqlite-and-libsqlite3-sys)
  describes bundled SQLite and its build-tool dependency.
