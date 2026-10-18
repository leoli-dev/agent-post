#!/usr/bin/env python3
"""Check local document links and public-document hygiene using the standard library."""

from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
PERSONAL_PATH = re.compile(r"/(?:Users|home)/[^/\s<>]+/")
errors = []
documents = sorted(ROOT.rglob("*.md"))

for path in ROOT.rglob("*"):
    if not path.is_file() or ".git" in path.relative_to(ROOT).parts:
        continue
    if path.suffix not in {".md", ".yml", ".yaml", ".py", ".toml"} and path.name not in {"LICENSE", ".gitignore", ".gitattributes", ".editorconfig"}:
        continue
    content = path.read_text(encoding="utf-8")
    relative = path.relative_to(ROOT)
    if PERSONAL_PATH.search(content):
        errors.append(f"{relative}: personal home-directory path")
    if content and not content.endswith("\n"):
        errors.append(f"{relative}: missing final newline")
    for number, line in enumerate(content.splitlines(), 1):
        trailing = line[len(line.rstrip()):]
        if trailing and not (path.suffix == ".md" and trailing == "  " and line.rstrip()):
            errors.append(f"{relative}:{number}: trailing whitespace")

for document in documents:
    if ".git" in document.relative_to(ROOT).parts:
        continue
    body = document.read_text(encoding="utf-8")
    relative = document.relative_to(ROOT)
    if body.count("```") % 2:
        errors.append(f"{relative}: unclosed Markdown fence")
    for match in LINK.finditer(body):
        raw = match.group(1).strip()
        target = raw[1:raw.index(">")] if raw.startswith("<") else raw.split()[0]
        parsed = urlsplit(target)
        if parsed.scheme or parsed.netloc or not parsed.path:
            continue
        path = unquote(parsed.path)
        resolved = (ROOT / path.lstrip("/") if path.startswith("/") else document.parent / path).resolve()
        if not resolved.is_relative_to(ROOT):
            errors.append(f"{relative}: link leaves repository: {target}")
        elif not resolved.exists():
            errors.append(f"{relative}: broken local link: {target}")

requirements = (ROOT / "docs/requirements.md").read_text(encoding="utf-8")
for number in range(1, 56):
    if f"REQ-{number:02d}" not in requirements:
        errors.append(f"docs/requirements.md: missing REQ-{number:02d}")

if errors:
    print("\n".join(errors), file=sys.stderr)
    raise SystemExit(1)
print(f"Documentation checks passed: {len(documents)} Markdown files; REQ-01..REQ-55 present.")
