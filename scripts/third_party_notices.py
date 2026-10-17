#!/usr/bin/env python3
"""Export module license notices from the actual local executable dependency graph."""
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]

def objects(raw):
    decoder = json.JSONDecoder()
    while raw.strip():
        raw = raw.lstrip()
        value, end = decoder.raw_decode(raw)
        yield value
        raw = raw[end:]

packages = subprocess.check_output(
    ['go', 'list', '-deps', '-json', './cmd/agent-post'], cwd=ROOT, text=True)
modules = {}
for package in objects(packages):
    module = package.get('Module', {})
    if module and not module.get('Main'):
        modules[module['Path']] = module

sections = ['Third-party licenses for Agent Post\n\n'
            'Generated from the dependency graph of ./cmd/agent-post.\n'
            'The Go toolchain and each dependency retain their respective terms.\n']
rows = []
for name, module in sorted(modules.items()):
    directory = Path(module['Dir'])
    licenses = sorted(path for path in directory.iterdir() if path.is_file()
                      and path.name.upper().startswith(('LICENSE', 'NOTICE', 'COPYING', 'COPYRIGHT')))
    if not licenses:
        raise SystemExit(f'No top-level license notice found for {name}')
    version = module['Version']
    rows.append(f'| `{name}` | `{version}` |')
    for path in licenses:
        sections.append(f'\n{"=" * 72}\n{name} {version} / {path.name}\n{"=" * 72}\n\n'
                        + path.read_text(encoding='utf-8').rstrip() + '\n')
goroot = Path(subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip())
for name in ['LICENSE', 'PATENTS']:
    path = goroot / name
    if path.exists():
        sections.append(f'\n{"=" * 72}\nGo toolchain / {name}\n{"=" * 72}\n\n' + path.read_text().rstrip() + '\n')
(ROOT / 'THIRD_PARTY_LICENSES.txt').write_text(''.join(sections), encoding='utf-8')
(ROOT / 'THIRD_PARTY_NOTICES.md').write_text(
    '# Third-party notices\n\n'
    'Agent Post source uses the [MIT license](LICENSE). Dependencies keep their own terms. '
    'The complete exported notices, including Go, the MCP SDK licensing transition, SQLite, '
    'SQLite extensions, and modernc third-party notices, are in '
    '[THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt). Include both files with distributed binaries.\n\n'
    'This inventory describes modules linked by `go list -deps ./cmd/agent-post` on the build host. '
    'It includes the full top-level license and notice files supplied by those modules; '
    'a new dependency or target build requires regenerating and reviewing the inventory. '
    'Upstream documentation is not copied into this project.\n\n'
    '| Module | Pinned version |\n| --- | --- |\n' + '\n'.join(rows) + '\n\n'
    'Regenerate after resolving the pinned modules:\n\n'
    '```sh\npython3 scripts/third_party_notices.py\n```\n', encoding='utf-8')
print(f'Exported notices for {len(modules)} dependency modules and the Go toolchain.')
