#!/usr/bin/env python3
"""Check the local Markdown links of the architecture documentation.

Every ``[text](target)`` link in the checked files must resolve to a file in
this repository, and a ``#fragment`` must match a heading of that file. Links
with a URI scheme (``https:``, ``mailto:``) are external and are skipped, and
so is a bare fragment, which only names a heading of the same file.

Usage: ``python3 scripts/check-architecture-links.py [file ...]``. With no
argument every ``architecture/*.md`` file is checked. The script exits with
status 1 and prints one line per broken link.
"""

from __future__ import annotations

import glob
import re
import sys
import unicodedata
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent

LINK_RE = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
HEADING_RE = re.compile(r"^#{1,6}\s+(.*?)\s*$")
SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")


def github_slug(heading: str) -> str:
    """Return the GitHub anchor of one heading.

    The rules are: strip inline Markdown emphasis and code markers, lower-case,
    drop every character that is not a letter, digit, space, hyphen or
    underscore, and replace spaces with hyphens.
    """
    text = heading.strip()
    text = re.sub(r"[`*_]", "", text)
    text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", text)
    slug = unicodedata.normalize("NFKD", text).lower()
    slug = re.sub(r"[^\w\s-]", "", slug, flags=re.UNICODE)
    slug = re.sub(r"\s", "-", slug)
    return slug


def anchors(path: Path) -> set[str]:
    """Return the anchor set of one Markdown file, with duplicate suffixes."""
    seen: dict[str, int] = {}
    result: set[str] = set()
    for line in path.read_text(encoding="utf-8").splitlines():
        match = HEADING_RE.match(line)
        if not match:
            continue
        slug = github_slug(match.group(1))
        count = seen.get(slug, 0)
        seen[slug] = count + 1
        result.add(slug if count == 0 else f"{slug}-{count}")
    return result


def display(path: Path) -> str:
    """Return a repository-relative path, or the path itself when outside."""
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def split_target(target: str) -> tuple[str, str]:
    """Split a link target into its path and fragment."""
    target = target.strip()
    if "#" in target:
        path, _, fragment = target.partition("#")
        return path, fragment
    return target, ""


def check_file(path: Path) -> list[str]:
    problems: list[str] = []
    cache: dict[Path, set[str]] = {}
    for line_number, line in enumerate(
        path.read_text(encoding="utf-8").splitlines(), start=1
    ):
        for target in LINK_RE.findall(line):
            target = target.strip()
            if not target or SCHEME_RE.match(target):
                continue
            link_path, fragment = split_target(target)
            resolved = (
                path if not link_path else (path.parent / link_path).resolve()
            )
            if link_path and not resolved.exists():
                problems.append(
                    f"{display(path)}:{line_number}: missing link target {target!r}"
                )
                continue
            if not fragment:
                continue
            if resolved not in cache:
                cache[resolved] = anchors(resolved)
            if fragment not in cache[resolved]:
                problems.append(
                    f"{display(path)}:{line_number}: "
                    f"missing heading #{fragment} in {display(resolved)}"
                )
    return problems


def main(argv: list[str]) -> int:
    if argv:
        files = [Path(name) for name in argv]
    else:
        files = sorted(Path(p) for p in glob.glob(str(REPO_ROOT / "architecture" / "*.md")))
    problems: list[str] = []
    for path in files:
        if not path.exists():
            problems.append(f"{path}: no such file")
            continue
        problems.extend(check_file(path))
    for problem in problems:
        print(problem)
    if problems:
        print(f"{len(problems)} broken link(s)")
        return 1
    print(f"{len(files)} file(s) checked, every local link resolves")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
