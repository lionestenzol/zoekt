// Copyright 2026. Windows-port addition (not upstream).
//
// scipBatchParser drives scip-ctags in one-shot BATCH mode (one process per
// file), mirroring batch.go's universal-ctags handling. Upstream go-ctags keeps
// a single scip-ctags process alive and streams every file through one
// persistent stdin/stdout pipe; that transport deadlocks on Windows (see
// batch.go for the universal-ctags root-cause chain - the child's replies never
// flush back across the pipe and zoekt blocks per file until its 60s timeout).
//
// scip-ctags speaks the universal-ctags interactive protocol (a "generate-tags"
// request on stdin, newline-delimited JSON replies on stdout, terminated by a
// "completed" reply). It DOES flush after every request
// (syntax-analysis/src/ctags.rs ctags_runner), so the protocol itself is sound
// on Windows - only go-ctags' persistent-pipe transport is the problem. We keep
// the tool and swap the transport: spawn scip-ctags per file, feed exactly one
// request, close stdin, drain stdout to EOF. Closing stdin makes the child's
// read loop see EOF and exit cleanly. Correctness-equivalent; the only cost is a
// process spawn per file, paid at index time - the same tradeoff batch.go makes.
//
// Content is fed over stdin (the protocol carries the bytes), so unlike
// universal-ctags batch mode we do not depend on the file existing on disk. The
// filename is used only for extension-based language detection and as the tag
// path.
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

func newScipBatchParser(bin string) goctags.Parser { return &scipBatchParser{bin: bin} }

type scipBatchParser struct {
	bin string
}

// scipRequest is the "generate-tags" command scip-ctags reads from stdin. It
// mirrors syntax-analysis/src/ctags.rs Request::GenerateTags (serde tag
// "command", kebab-case). size is the exact byte length of the content that
// follows the request line, which the child reads with read_exact.
type scipRequest struct {
	Command  string `json:"command"`
	Filename string `json:"filename"`
	Size     int    `json:"size"`
}

// scipReply mirrors the JSON objects scip-ctags emits (one per line). We only
// consume "tag" replies; "program", "completed", and "error" are ignored.
// scip-ctags does not emit scopeKind or signature (see the Reply::Tag comment in
// ctags.rs), so ParentKind/Signature are left empty.
type scipReply struct {
	Typ      string `json:"_type"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
}

func (s *scipBatchParser) Parse(name string, content []byte) ([]*goctags.Entry, error) {
	req, err := json.Marshal(scipRequest{
		Command:  "generate-tags",
		Filename: name,
		Size:     len(content),
	})
	if err != nil {
		return nil, nil
	}

	// Payload = request line + newline + exactly len(content) content bytes. No
	// trailing newline: the child reads the content with read_exact, then the
	// next read_line hits EOF (stdin closed) and the process exits cleanly.
	var payload bytes.Buffer
	payload.Grow(len(req) + 1 + len(content))
	payload.Write(req)
	payload.WriteByte('\n')
	payload.Write(content)

	cmd := exec.Command(s.bin)
	cmd.Stdin = bytes.NewReader(payload.Bytes())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A file scip-ctags can't parse (unsupported extension, malformed
		// source) must not kill the shard. Degrade to "no symbols" rather than
		// erroring the whole index - same policy as batch.go.
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
		var r scipReply
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.Typ != "tag" || r.Line <= 0 || r.Name == "" {
			continue
		}
		entries = append(entries, &goctags.Entry{
			Name:     r.Name,
			Path:     name,
			Line:     r.Line,
			Kind:     r.Kind,
			Language: r.Language,
			Parent:   r.Scope,
		})
	}
	return entries, nil
}

func (s *scipBatchParser) Close() {}
