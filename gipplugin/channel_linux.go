//go:build linux

package gipplugin

import (
	"fmt"
	"syscall"
)

func dup2(oldfd, newfd int) error { return syscall.Dup3(oldfd, newfd, 0) }

const prSetDumpable = 4 // PR_SET_DUMPABLE, <linux/prctl.h>

// hardenProcess makes the plugin non-dumpable (prctl(PR_SET_DUMPABLE, 0)):
// /proc/<pid>/fd and /proc/<pid>/mem of the plugin can then be opened, and
// the plugin ptraced, only with CAP_SYS_PTRACE, so a same-user child
// cannot reopen the channel through /proc.
func hardenProcess() error {
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); e != 0 {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", e)
	}
	return nil
}
