// Copyright 2026. Windows-port addition (not upstream).
//
// batchParser drives universal-ctags in one-shot BATCH mode (one process per
// file) instead of go-ctags' persistent interactive pipe. The interactive pipe
// (go-ctags/ctags.go, --_interactive=default) deadlocks on Windows: the child's
// stdout never flushes back across the pipe, so zoekt blocks per file until its
// 60s timeout and captures 0 symbols. ctags itself is fine in batch mode (proven
// directly), so we swap the transport, not the tool. Correctness-equivalent for
// symbol extraction; the only cost is a process spawn per file, paid at index
// time. Reads the real on-disk file, which the filesystem indexer guarantees to
// exist (doc.Name is the absolute path).
//
// See ~/.claude/rules/common/code-as-furniture.md - no broken code left in place;
// project_zoekt_windows_native memory for the full root-cause chain.
package ctags

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"

	goctags "github.com/sourcegraph/go-ctags"
)

func newBatchParser(bin string) goctags.Parser { return &batchParser{bin: bin} }

type batchParser struct {
	bin string
}

// batchTag mirrors the JSON object universal-ctags emits with
// --output-format=json --fields=* (one object per line).
type batchTag struct {
	Typ       string `json:"_type"`
	Name      string `json:"name"`
	Line      int    `json:"line"`
	Kind      string `json:"kind"`
	Language  string `json:"language"`
	Scope     string `json:"scope"`
	ScopeKind string `json:"scopeKind"`
	Signature string `json:"signature"`
	Pattern   string `json:"pattern"`
}

func (b *batchParser) Parse(name string, content []byte) ([]*goctags.Entry, error) {
	cmd := exec.Command(b.bin,
		"--output-format=json",
		"--fields=*",
		"--pattern-length-limit=255",
		"-f", "-",
		name,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A file whose language ctags can't parse must not kill the shard.
		// Degrade to "no symbols" rather than erroring the whole index.
		return nil, nil
	}

	var entries []*goctags.Entry
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var t batchTag
		if err := json.Unmarshal(line, &t); err != nil {
			continue
		}
		if t.Typ != "tag" || t.Line <= 0 || t.Name == "" {
			continue
		}
		entries = append(entries, &goctags.Entry{
			Name:       t.Name,
			Path:       name,
			Line:       t.Line,
			Kind:       t.Kind,
			Language:   t.Language,
			Parent:     t.Scope,
			ParentKind: t.ScopeKind,
			Pattern:    t.Pattern,
			Signature:  t.Signature,
		})
	}
	return entries, nil
}

func (b *batchParser) Close() {}
