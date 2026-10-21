//go:build unix

package gipplugin

import (
	"fmt"
	"os"
	"syscall"
)

// isolateChannel moves the JSON-RPC channel off descriptors 0 and 1 (gip
// ADR-0025 §8, PLG-028). It duplicates them to private close-on-exec
// descriptors, which become the channel, then points descriptor 0 at the
// null device and descriptor 1 at stderr. os.Stdin and os.Stdout keep
// naming descriptors 0 and 1, so they now read the null device and write
// to stderr: no child can inherit the channel however its exec.Cmd is
// built (os.Stdin, os.Stdout, io.MultiWriter(os.Stdout, ...),
// os.NewFile(1, ...)), and a stray fmt.Println cannot corrupt the
// protocol. On Linux the process is also made non-dumpable, so a
// same-user process cannot reopen the channel through /proc/<pid>/fd.
func isolateChannel() (in, out *os.File, err error) {
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	inFd, err := syscall.Dup(0)
	if err != nil {
		return nil, nil, fmt.Errorf("dup stdin: %w", err)
	}
	syscall.CloseOnExec(inFd)
	outFd, err := syscall.Dup(1)
	if err != nil {
		syscall.Close(inFd)
		return nil, nil, fmt.Errorf("dup stdout: %w", err)
	}
	syscall.CloseOnExec(outFd)
	null, err := syscall.Open(os.DevNull, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		syscall.Close(inFd)
		syscall.Close(outFd)
		return nil, nil, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	err = dup2(null, 0)
	syscall.Close(null)
	if err == nil {
		err = dup2(2, 1)
	}
	if err != nil {
		// Descriptors 0 and 1 may be half moved; the channel is still
		// whole on inFd and outFd, but fail closed.
		syscall.Close(inFd)
		syscall.Close(outFd)
		return nil, nil, fmt.Errorf("move the channel off descriptors 0 and 1: %w", err)
	}
	if err := hardenProcess(); err != nil {
		syscall.Close(inFd)
		syscall.Close(outFd)
		return nil, nil, err
	}
	return os.NewFile(uintptr(inFd), "gip-channel-in"), os.NewFile(uintptr(outFd), "gip-channel-out"), nil
}
