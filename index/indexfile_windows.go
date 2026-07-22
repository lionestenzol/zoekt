//go:build windows

// Windows port of index/indexfile.go. The Unix version mmaps the shard file
// (unix.Mmap, which has no Windows equivalent in golang.org/x/sys); here we read
// the shard into memory instead. The IndexFile contract only requires
// Read/Size/Close/Name (see read.go), and Read is a pure slice into the buffer,
// so this is behavior-equivalent to the mmap version — it just trades address
// space for heap (fine for local/personal indexes). A future upgrade could use
// Windows CreateFileMapping/MapViewOfFile for true mmap.
//
// See ~/.claude/rules/common/code-as-furniture.md — the platform gap is filled,
// not documented-and-skipped.

package index

import (
	"fmt"
	"io"
	"math"
	"os"
)

type memIndexFile struct {
	name string
	size uint32
	data []byte
}

func (f *memIndexFile) Read(off, sz uint32) ([]byte, error) {
	if off > off+sz || off+sz > uint32(len(f.data)) {
		return nil, fmt.Errorf("out of bounds: %d, len %d, name %s", off+sz, len(f.data), f.name)
	}
	return f.data[off : off+sz], nil
}

func (f *memIndexFile) Name() string {
	return f.name
}

func (f *memIndexFile) Size() (uint32, error) {
	return f.size, nil
}

func (f *memIndexFile) Close() {}

// NewIndexFile returns a new index file. The index file takes ownership of the
// passed in file, and may close it.
func NewIndexFile(f *os.File) (IndexFile, error) {
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	sz := fi.Size()
	if sz >= math.MaxUint32 {
		return nil, fmt.Errorf("file %s too large: %d", f.Name(), sz)
	}

	data := make([]byte, sz)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}

	return &memIndexFile{name: f.Name(), size: uint32(sz), data: data}, nil
}
