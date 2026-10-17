# Third-party notices

Agent Post source uses the [MIT license](LICENSE). Dependencies keep their own terms. The complete exported notices, including Go, the MCP SDK licensing transition, SQLite, SQLite extensions, and modernc third-party notices, are in [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt). Include both files with distributed binaries.

This inventory describes modules linked by `go list -deps ./cmd/agent-post` on the build host. It includes the full top-level license and notice files supplied by those modules; a new dependency or target build requires regenerating and reviewing the inventory. Upstream documentation is not copied into this project.

| Module | Pinned version |
| --- | --- |
| `github.com/dustin/go-humanize` | `v1.0.1` |
| `github.com/google/jsonschema-go` | `v0.4.3` |
| `github.com/google/uuid` | `v1.6.0` |
| `github.com/mattn/go-isatty` | `v0.0.24` |
| `github.com/modelcontextprotocol/go-sdk` | `v1.8.0` |
| `github.com/ncruces/go-strftime` | `v1.0.0` |
| `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` |
| `github.com/segmentio/asm` | `v1.1.3` |
| `github.com/segmentio/encoding` | `v0.5.4` |
| `github.com/yosida95/uritemplate/v3` | `v3.0.2` |
| `golang.org/x/oauth2` | `v0.35.0` |
| `golang.org/x/sync` | `v0.23.0` |
| `golang.org/x/sys` | `v0.48.0` |
| `golang.org/x/time` | `v0.15.0` |
| `modernc.org/libc` | `v1.77.1` |
| `modernc.org/mathutil` | `v1.7.1` |
| `modernc.org/memory` | `v1.12.1` |
| `modernc.org/sqlite` | `v1.60.1` |

Regenerate after resolving the pinned modules:

```sh
python3 scripts/third_party_notices.py
```
