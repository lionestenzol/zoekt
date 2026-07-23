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

## scip-ctags — tree-sitter symbols for TS/TSX

universal-ctags is regex-based and tags TypeScript poorly: on a real
`delta-kernel/cockpit.ts` it caught only **3 of 15** exported functions (it skips
multi-line signatures — see Caveat). Sourcegraph's **scip-ctags** is tree-sitter
based and gets **15/15**. This branch routes `.ts`/`.tsx` to scip-ctags on Windows
(`internal/ctags/scip_batch.go` + `parser.go`, commit `9fd7b5c`); everything else
stays on universal-ctags.

scip-ctags speaks the same interactive protocol as universal-ctags, which
deadlocks over a persistent pipe on Windows — so, exactly like `batch.go`, we
spawn it **once per file** (write one `generate-tags` request + content on stdin,
close stdin, drain the JSON tag replies). This is safe because scip-ctags flushes
stdout after every request.

### Building `scip-ctags.exe`

It is not vendored here (34 MB binary). Build it from Sourcegraph's monorepo — a
sparse+shallow checkout of just the one crate keeps the clone at ~10 MB:

```sh
git clone --filter=blob:none --sparse --depth 1 \
  https://github.com/sourcegraph/sourcegraph-public-snapshot C:/Users/bruke/scip-ctags-src
cd C:/Users/bruke/scip-ctags-src
git sparse-checkout set docker-images/syntax-highlighter
cd docker-images/syntax-highlighter
# rust-toolchain.toml pins Rust 1.78.0; rustup installs it automatically.
# Needs MSVC build tools (tree-sitter C grammars link against them).
cargo build --release --bin scip-ctags
cp target/release/scip-ctags.exe C:/Users/bruke/zoekt-win/scip-ctags.exe
```

A release build needs ~3-4 GB of free disk for `target/`. Note the bin lives in
the **root** package (`--bin scip-ctags`); `scip-syntax` is a separate workspace
crate (`-p scip-syntax`) and is not needed here.

### Wiring it in

zoekt honors a `SCIP_CTAGS_COMMAND` env override (mirrors `CTAGS_COMMAND`).
`reindex.py` sets it to `C:\Users\bruke\zoekt-win\scip-ctags.exe` when that file
exists; if it's missing, TS/TSX silently fall back to universal-ctags. Routing is
by go-enry **language** name, not extension: only `TypeScript` and `TSX` go to
scip. JavaScript is deliberately left on universal-ctags — go-enry lumps
`.js/.jsx/.mjs/.cjs` all as `JavaScript`, but scip-ctags only recognizes the `.js`
extension, so routing the whole language would drop symbols for the rest.

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

For languages still on universal-ctags, `sym:` inherits its limitation: it skips
definitions whose signature **wraps across multiple lines** (`export async
function foo(` + params on the next lines → 0 tags). `.ts`/`.tsx` no longer suffer
this — scip-ctags (tree-sitter) handles multi-line signatures. For everything
else, an empty `sym:` is not proof the symbol is undefined — fall back to a
plain/regex identifier search.
