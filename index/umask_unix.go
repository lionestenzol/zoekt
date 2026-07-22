//go:build linux || darwin || freebsd || netbsd

package index

import (
	"os"

	"golang.org/x/sys/unix"
)

// On Unix, read the process umask so created shard files get correct perms.
// Split out of builder.go for the Windows port (Windows has no umask concept;
// the umask var stays 0). See ~/.claude/rules/common/code-as-furniture.md.
func init() {
	umask = os.FileMode(unix.Umask(0))
	unix.Umask(int(umask))
}
