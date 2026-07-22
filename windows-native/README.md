# zoekt — native Windows port (no WSL, no Docker)

Upstream `sourcegraph/zoekt` doesn't build on Windows: the `index` package is
Unix-only (`mmap`, `umask`) and its ctags driver deadlocks on Windows pipes.
This branch (`windows-port`) makes the three CLIs — `zoekt`, `zoekt-index`,
`zoekt-git-index` — build and run natively on `windows/amd64`, with working
trigram **and** symbol (`sym:`) search.

## The port — 5 files, 2 commits

**Platform (commit `aaf5d3fe`)**
- `index/indexfile_windows.go` (NEW, `//go:build windows`) — memory-backed
  `NewIndexFile`: reads the shard into a `[]byte` instead of `unix.Mmap`. The
  `IndexFile` interface only needs Read/Size/Close/Name, so it's behavior-
  equivalent (trades address space for heap; fine for local indexes).
- `index/umask_unix.go` (NEW, unix build tag) — moves the umask `init()` here;
  the `umask` var defaults to 0 on Windows.
- `index/builder.go` (EDIT) — drops the now-unused `golang.org/x/sys/unix` import.

**Symbols / ctags (commit `feat(ctags): drive universal-ctags in batch mode`)**
- `internal/ctags/batch.go` (NEW) — go-ctags drives universal-ctags as one
  persistent **interactive** pipe (`--_interactive=default`); on Windows the
  child's stdout never flushes back, so zoekt blocks per file until its 60s
  timeout and captures 0 symbols. ctags itself is fine in batch mode. This
  parser runs one `ctags` process per file against the real on-disk path.
- `internal/ctags/parser.go` (EDIT) — route to `batchParser` when
  `runtime.GOOS == "windows"`.

Proven: **2588 symbols / 2s** where interactive gave 0 / 60s.

## Build

```sh
cd <repo>
CGO_ENABLED=0 go build -o C:/Users/bruke/go/bin/zoekt.exe           ./cmd/zoekt
CGO_ENABLED=0 go build -o C:/Users/bruke/go/bin/zoekt-index.exe     ./cmd/zoekt-index
CGO_ENABLED=0 go build -o C:/Users/bruke/go/bin/zoekt-git-index.exe ./cmd/zoekt-git-index
```

(`go/bin` is on PATH, so the binaries are callable bare.) Use `zoekt-index`
(filesystem walk), not `zoekt-git-index`: the go-git backend rejects repos with
the `worktreeconfig` extension, which git-worktree repos use.

## ctags on Windows

zoekt resolves ctags via `exec.LookPath("universal-ctags")`, but Windows names
the binary `ctags.exe`. zoekt honors a `CTAGS_COMMAND` env override — point it at
the real binary (e.g. `C:\mingw64\bin\ctags.exe`). `reindex.py` sets this for you.
universal-ctags must be built with `+interactive` (ours is; batch mode uses it via
`--output-format=json`).

## Tooling in this folder

- **`reindex.py`** — repeatable index builder. Source-vs-junk ignore set (derived
  from git's ignored dirs minus basenames that hold tracked source), sets
  `CTAGS_COMMAND`, symbols on by default. `python reindex.py [repo-substring]`.
- **`zoekt_mcp.py`** — FastMCP stdio server exposing `zoekt_search` / `zoekt_status`
  to Claude Code. Register: `claude mcp add zoekt -s user -- python <path>/zoekt_mcp.py`.

> Canonical live copies of these two run from `C:\Users\bruke\zoekt-win\` (the MCP
> is registered against that path). The copies here are the preserved snapshot;
> keep them in sync if the live ones change.

## Caveat

`sym:` inherits universal-ctags' limitation: it skips definitions whose signature
**wraps across multiple lines** (`export async function foo(` + params on the next
lines → 0 tags). An empty `sym:` is not proof the symbol is undefined — fall back
to a plain/regex identifier search.
