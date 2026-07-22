#!/usr/bin/env python3
"""Zoekt MCP server - exposes native-Windows zoekt code search as MCP tools.

Wraps the ported native `zoekt.exe` (see project_zoekt_windows_native memory) so an
MCP client (Claude Desktop / Claude Code) can do fast, symbol-aware trigram search
over Bruke's big code repos as a first-class tool. No Docker, no WSL, no Sourcegraph
server. Talks MCP over stdio - same pattern as atlas-rag/atlas_mcp.py.

Index is built by zoekt-git-index / zoekt-index into ZOEKT_INDEX_DIR (see reindex.py).

Run directly:  python zoekt_mcp.py
Register:      claude mcp add zoekt -s user -- C:\\Python313\\python.exe <abs>\\zoekt_mcp.py
"""
import os
import subprocess
from pathlib import Path

from mcp.server.fastmcp import FastMCP

# Absolute path to the ported binary so we don't depend on the MCP client's PATH.
ZOEKT_BIN = os.environ.get("ZOEKT_BIN", r"C:\Users\bruke\go\bin\zoekt.exe")
INDEX_DIR = os.environ.get("ZOEKT_INDEX_DIR", r"C:\Users\bruke\zoekt-win\index")

mcp = FastMCP("zoekt")


def _index_ready() -> bool:
    d = Path(INDEX_DIR)
    return d.is_dir() and any(d.glob("*.zoekt"))


def _run_zoekt(args: list[str], timeout: int = 30) -> tuple[int, str, str]:
    try:
        p = subprocess.run(
            [ZOEKT_BIN, "-index_dir", INDEX_DIR, *args],
            capture_output=True, text=True, timeout=timeout,
        )
        return p.returncode, p.stdout, p.stderr
    except FileNotFoundError:
        return 127, "", f"zoekt binary not found at {ZOEKT_BIN}"
    except subprocess.TimeoutExpired:
        return 124, "", f"zoekt timed out after {timeout}s"


@mcp.tool()
def zoekt_search(query: str, max_results: int = 60) -> str:
    """Fast trigram code search over Bruke's indexed big code repos (Pre Atlas,
    delta-scp/pre-atlas, STRUDEL). Returns matching `file:line:text` lines, ranked
    (symbol definitions outrank plain text hits, via universal-ctags).

    The query uses zoekt's query language, not plain grep. Useful forms:
      - bare text / RE2 regex:  `checkTimeouts`  ·  `func \\w+Handler`
      - symbol definitions:     `sym:WorkController`   (ranked by ctags)
      - restrict by file path:  `file:delta-kernel checkTimeouts`
      - restrict by language:   `lang:typescript optimisticUpdate`
      - case sensitive:         `case:yes Foo`
      - boolean:                `sym:retrieve lang:python`
    Combine terms with spaces (AND). Prefer this over guessing where code lives.

    Args:
        query: a zoekt query (see forms above).
        max_results: cap on lines returned (default 60).
    """
    if not _index_ready():
        return (f"No index at {INDEX_DIR}. Build it with "
                f"`python C:\\Users\\bruke\\zoekt-win\\reindex.py`.")
    code, out, err = _run_zoekt([query])
    if code == 124 or code == 127:
        return err
    out = out.strip()
    if not out:
        return f"No results for: {query}"
    lines = out.splitlines()
    n = max(1, min(int(max_results), 500))
    body = "\n".join(lines[:n])
    if len(lines) > n:
        body += f"\n... ({len(lines) - n} more lines truncated; refine the query)"
    return body


@mcp.tool()
def zoekt_status() -> str:
    """Report what is indexed: the index dir, its shard files, and their repos.
    Use this to see whether a repo is searchable before running zoekt_search."""
    d = Path(INDEX_DIR)
    if not d.is_dir():
        return f"Index dir {INDEX_DIR} does not exist yet."
    shards = sorted(d.glob("*.zoekt"))
    if not shards:
        return f"Index dir {INDEX_DIR} exists but has no .zoekt shards yet."
    total = sum(s.stat().st_size for s in shards)
    rows = "\n".join(f"  {s.name}  ({s.stat().st_size // 1024} KB)" for s in shards)
    return (f"Index: {INDEX_DIR}\n{len(shards)} shard(s), {total // (1024*1024)} MB total:\n"
            f"{rows}\n\nSearch with zoekt_search(query). Rebuild with reindex.py.")


if __name__ == "__main__":
    mcp.run()
