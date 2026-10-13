#!/usr/bin/env python3
"""Cheap documentation decision before installing Go; no guards are executed."""
import os
import subprocess
from pathlib import Path

base = os.environ.get("CI_BASE_SHA", "")
needed = True
if base:
    result = subprocess.run(["git", "diff", "--name-only", "-z", base, "HEAD"],
                            check=True, stdout=subprocess.PIPE)
    files = [p.decode() for p in result.stdout.split(b"\0") if p]
    # Markdown only, or issue/PR templates, are documentation. Test fixtures
    # and any executable file under docs/ remain code changes.
    def documentation(p):
        parts = Path(p).parts
        return ((p.endswith(".md") and (len(parts) == 1 or parts[0] == "docs"))
                or p.startswith(".github/ISSUE_TEMPLATE/")
                or p == ".github/PULL_REQUEST_TEMPLATE.md")
    needed = any(not documentation(p) for p in files)
value = "true" if needed else "false"
print("Fast Go validation required: " + value)
if os.environ.get("GITHUB_OUTPUT"):
    with open(os.environ["GITHUB_OUTPUT"], "a") as out:
        out.write("code=" + value + "\n")
