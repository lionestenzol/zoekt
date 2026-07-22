#!/usr/bin/env python3
"""Rebuild the zoekt index over Bruke's big code repos.

Uses zoekt-index (filesystem walk) rather than zoekt-git-index because the go-git
backend rejects repos with the `worktreeconfig` extension (Pre Atlas + STRUDEL use
git worktrees). The filesystem indexer is worktree-agnostic; IGNORE_DIRS + FILE_LIMIT
keep node_modules/.git/build/data junk and huge files out.

IGNORE_DIRS provenance (source-vs-junk logic, borrowed from code-recon / git itself):
zoekt-index matches ignore names by BASENAME at any depth (cmd/zoekt-index/main.go:51),
so one name covers every nested copy. The junk names below are git's own ignored-dir
basenames MINUS any basename that also holds tracked source. Concretely, `data`,
`fixtures`, `skills`, `tools` are DELIBERATELY NOT ignored: Pre Atlas tracks real source
under dirs with those names, and a basename ignore would wrongly drop it. Everything else
git ignores (deps, caches, build output, egg-info, runtime state) is safe to drop.
Deriving the set: compare `git status --ignored` dir basenames against `git ls-files`
path segments; ignore only the basenames with zero tracked source underneath.

ctags / symbol ranking (the `sym:` query) is ON by default. zoekt finds ctags via
CTAGS_COMMAND (index/builder.go:288) - set below to our real binary since Windows has no
`universal-ctags` on PATH. Upstream go-ctags drives ctags as a single persistent
interactive pipe (--_interactive=default) which DEADLOCKS on Windows (60s timeout, 0
symbols per file - the child's stdout never flushes back). Our windows-port fixes this in
src/internal/ctags/batch.go: on Windows we drive ctags in one-shot BATCH mode (one process
per file, reading the real on-disk file). Proven: 2588 symbols/2s where interactive gave
0/60s. Set ZOEKT_CTAGS=0 to skip symbols (content-only, marginally faster). Plain / regex /
file: / lang: search never need ctags and always work.
See project_zoekt_windows_native memory.

    python C:\\Users\\bruke\\zoekt-win\\reindex.py              # all repos, with symbols
    python C:\\Users\\bruke\\zoekt-win\\reindex.py "Pre Atlas"  # only repos matching arg
    ZOEKT_CTAGS=0 python C:\\Users\\bruke\\zoekt-win\\reindex.py  # content-only, no symbols
"""
import os
import subprocess
import sys
from pathlib import Path

INDEX_BIN = os.environ.get("ZOEKT_INDEX_BIN",
                           r"C:\Users\bruke\go\bin\zoekt-index.exe")
INDEX_DIR = os.environ.get("ZOEKT_INDEX_DIR", r"C:\Users\bruke\zoekt-win\index")
# Real universal-ctags binary (Windows names it ctags.exe, not universal-ctags).
CTAGS_COMMAND = os.environ.get("CTAGS_COMMAND", r"C:\mingw64\bin\ctags.exe")
# Symbols ON by default - Windows uses batch-mode ctags (src/internal/ctags/batch.go),
# which sidesteps the go-ctags interactive-pipe deadlock. Set ZOEKT_CTAGS=0 to skip.
CTAGS_ON = os.environ.get("ZOEKT_CTAGS", "1") != "0"

# Directories never worth indexing. Basename match at any depth. Source-vs-junk = git's
# ignored dirs minus basenames that also hold tracked source (data/fixtures/skills/tools).
IGNORE_DIRS = ",".join([
    # VCS + dependency + build/output + cache (generic)
    ".git", ".hg", ".svn", "node_modules", "vendor", "coverage",
    "dist", "build", "out", "output", ".next", ".wasp", ".audit",
    "target", ".cache", ".parcel-cache", ".turbo",
    # python caches / venvs
    "__pycache__", ".venv", "venv", ".mypy_cache", ".pytest_cache", ".ruff_cache",
    # runtime state held open by running Atlas services (locked files abort a walk)
    "tmp", ".tmp", "logs", ".logs", ".atlas-logs", ".atlas",
    ".aegis-data", ".canvas-sessions", ".claire", ".delta-fabric", ".delta-scp",
    ".groundwork", ".playwright-mcp", ".seam", ".state", ".weapon", ".weapon-lsh-wire",
    # worktrees (both spellings)
    "worktrees", ".worktrees",
    # data / generated corpora / retired output (git-ignored, zero tracked source)
    "anatomy-research", "backups", "fuzz-corpus", "genesis_output", "harvest",
    "markdown_output", "openscreen", "routing-png", "test-results", "var",
    # repo-specific DATA corpora - tracked but not code (a code index should not
    # spawn ctags on them). STRUDEL: hh_trp_json = 15k JSON trap-dataset records
    # (queryable via its own hh_trp_index.sqlite); .loop-history = editor snapshots.
    "hh_trp_json", ".loop-history",
    # python packaging metadata
    "atlas_cli.egg-info", "atlas_map_api.egg-info", "cortex.egg-info",
    "memory_hub.egg-info", "optogon.egg-info", "perception.egg-info",
    "search_stack.egg-info", "triangulation.egg-info",
    # a literal '~' dir left by a tool under services/_retired
    "~",
])
FILE_LIMIT = "2000000"  # skip files > 2 MB (generated/minified/data blobs)

# Big code repos. Edit this list to change what's searchable.
REPOS = [
    r"C:\Users\bruke\Pre Atlas",
    r"C:\Users\bruke\pre-atlas",
    r"C:\Users\bruke\STRUDEL",
]


def clean_stale_tmp() -> None:
    """A timed-out/aborted run leaves *.zoekt.*.tmp shards (can be ~GB). Drop them."""
    for tmp in Path(INDEX_DIR).glob("*.zoekt.*.tmp"):
        try:
            tmp.unlink()
            print(f"  removed stale tmp shard: {tmp.name}")
        except OSError as e:
            print(f"  WARN could not remove {tmp.name}: {e}")


def main() -> int:
    Path(INDEX_DIR).mkdir(parents=True, exist_ok=True)
    clean_stale_tmp()

    only = sys.argv[1] if len(sys.argv) > 1 else None
    repos = [r for r in REPOS if (only is None or only.lower() in r.lower())]
    if only and not repos:
        print(f"No repo in REPOS matches {only!r}. Known: {REPOS}")
        return 1

    env = dict(os.environ)
    env["CTAGS_COMMAND"] = CTAGS_COMMAND
    cmd_base = [INDEX_BIN, "-index", INDEX_DIR,
                "-ignore_dirs", IGNORE_DIRS, "-file_limit", FILE_LIMIT]
    if not CTAGS_ON:
        cmd_base.append("-disable_ctags")
    print(f"ctags: {'ON (symbols)' if CTAGS_ON else 'off (content-only)'}"
          f"  |  {len(repos)} repo(s)")

    ok, fail = 0, 0
    for repo in repos:
        if not Path(repo).is_dir():
            print(f"SKIP (not a directory): {repo}")
            fail += 1
            continue
        print(f"=== indexing {repo} ===")
        p = subprocess.run(cmd_base + [repo], capture_output=True, text=True, env=env)
        sys.stdout.write(p.stdout[-800:])
        if p.returncode == 0:
            ok += 1
        else:
            sys.stderr.write(p.stderr[-800:])
            print(f"[FAILED rc={p.returncode}]")
            fail += 1

    shards = list(Path(INDEX_DIR).glob("*.zoekt"))
    total_mb = sum(s.stat().st_size for s in shards) // (1024 * 1024)
    print(f"\nDone: {ok} repo(s) indexed, {fail} skipped/failed. "
          f"{len(shards)} shard(s), {total_mb} MB in {INDEX_DIR}")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
