package server

import (
	"os"
	"strings"
)

// procSelfExe refers to the running binary, even after it was replaced on disk.
const procSelfExe = "/proc/self/exe"

// restartPending reports whether the gum binary on disk was replaced since this
// process started, e.g., by a package upgrade, which deliberately doesn't
// restart gum. Var so tests can stub it.
var restartPending = func() bool {
	target, err := os.Readlink(procSelfExe)
	if err != nil {
		return false
	}
	// The kernel appends " (deleted)" once the running file was unlinked.
	return replaced(procSelfExe, strings.TrimSuffix(target, " (deleted)"))
}

// replaced reports whether runningPath and diskPath are different files. It
// returns false if either can't be stat'ed, e.g., because the binary was
// removed: then a restart wouldn't help.
func replaced(runningPath, diskPath string) bool {
	running, err := os.Stat(runningPath)
	if err != nil {
		return false
	}
	onDisk, err := os.Stat(diskPath)
	if err != nil {
		return false
	}
	return !os.SameFile(running, onDisk)
}
