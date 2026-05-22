#!/usr/bin/env python3
"""
Extract checksuites from hardening-guide Markdown files and produce
a checksuites.yaml compatible with the hardener --ruleset flag.

Usage:
  python3 extract_checksuites.py <sections_dir> [output.yaml]

Examples:
  python3 extract_checksuites.py ../linux-hardening-guide/report/sections
  python3 extract_checksuites.py ../macos-hardening-guide/report/sections macos.yaml
"""

import sys
import os
import yaml


def parse_frontmatter(text: str) -> dict | None:
    """Return the parsed YAML frontmatter dict, or None if the file has none."""
    lines = text.split("\n")
    delimiters = [i for i, l in enumerate(lines) if l.strip() == "---"]
    if len(delimiters) < 2:
        return None
    fm_text = "\n".join(lines[delimiters[0] + 1 : delimiters[1]])
    return yaml.safe_load(fm_text)


def _literal_representer(dumper: yaml.Dumper, data: str) -> yaml.ScalarNode:
    """Use literal block scalar (|) for multi-line strings."""
    if "\n" in data:
        return dumper.represent_scalar("tag:yaml.org,2002:str", data, style="|")
    return dumper.represent_scalar("tag:yaml.org,2002:str", data)


def build_dumper() -> type:
    """Return a Dumper subclass that writes multi-line strings as | blocks."""
    dumper = yaml.Dumper
    dumper.add_representer(str, _literal_representer)
    return dumper


def read_preamble(sections_dir: str) -> dict:
    """
    Parse 00_README.md and return the fields that apply guide-wide:
    os, arch, and preconditions.  Returns an empty dict if the file
    is absent or has no relevant frontmatter.
    """
    readme_path = os.path.join(sections_dir, "00_README.md")
    if not os.path.exists(readme_path):
        return {}
    with open(readme_path, "r", encoding="utf-8") as fh:
        fm = parse_frontmatter(fh.read())
    if not fm:
        return {}
    preamble = {}
    if fm.get("os"):
        preamble["os"] = fm["os"]
    if isinstance(fm.get("arch"), list):
        preamble["arch"] = fm["arch"]
    tools = fm.get("preconditions", {})
    if isinstance(tools, dict) and tools.get("tools"):
        preamble["preconditions"] = {"tools": tools["tools"]}
    return preamble


def extract(sections_dir: str, output_file: str) -> None:
    try:
        filenames = sorted(
            f
            for f in os.listdir(sections_dir)
            if f.endswith(".md") and not f.startswith(".")
        )
    except FileNotFoundError:
        print(f"error: directory not found: {sections_dir}", file=sys.stderr)
        sys.exit(1)

    # Guide-wide defaults from 00_README.md (os, arch, preconditions).
    # Preconditions are added to every suite so that LoadRuleset gates on them
    # regardless of which suite the user picks in the interactive prompt.
    preamble = read_preamble(sections_dir)
    if preamble.get("preconditions"):
        print(f"Preamble preconditions: {preamble['preconditions']['tools']}")

    docs: list[dict] = []

    for filename in filenames:
        if filename == "00_README.md":
            continue  # preamble already consumed above

        path = os.path.join(sections_dir, filename)
        with open(path, "r", encoding="utf-8") as fh:
            content = fh.read()

        fm = parse_frontmatter(content)
        if fm is None:
            continue

        # "checksuites" is the canonical key in the hardening-guide MD files.
        # The "checks" key in the same frontmatter belongs to the template engine
        # (checks.engine.templatenotfound) and must NOT be used here.
        checksuites = fm.get("checksuites")
        if not checksuites or not isinstance(checksuites, list):
            continue

        doc: dict = {"title": fm.get("title") or filename}

        # os: prefer per-file value, fall back to preamble
        os_val = fm.get("os") or preamble.get("os")
        if os_val:
            doc["os"] = os_val

        # arch: prefer per-file value, fall back to preamble
        arch_val = fm.get("arch") if isinstance(fm.get("arch"), list) else preamble.get("arch")
        if arch_val:
            doc["arch"] = arch_val

        # preconditions live in the guide header, not per-suite
        doc["checksuites"] = checksuites
        docs.append(doc)

    if not docs:
        print("No checksuites found in any Markdown file.", file=sys.stderr)
        sys.exit(1)

    dumper = build_dumper()

    with open(output_file, "w", encoding="utf-8") as out:
        # Write the guide-level header document if there are any preconditions.
        # LoadRuleset detects it as the first title-less document.
        if preamble.get("preconditions"):
            header = {"preconditions": preamble["preconditions"]}
            yaml.dump(header, out, Dumper=dumper, default_flow_style=False,
                      allow_unicode=True, sort_keys=False, width=120)
            out.write("\n---\n\n")

        for i, doc in enumerate(docs):
            if i > 0:
                out.write("\n---\n\n")
            yaml.dump(doc, out, Dumper=dumper, default_flow_style=False,
                      allow_unicode=True, sort_keys=False, width=120)

    print(f"Wrote {len(docs)} suites -> {output_file}")


if __name__ == "__main__":
    sections = sys.argv[1] if len(sys.argv) > 1 else "."
    output = sys.argv[2] if len(sys.argv) > 2 else "checksuites.yaml"
    extract(sections, output)
