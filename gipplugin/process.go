package gipplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// The JSON-RPC channel is the only way to the host, and so to the plugin's
// data (gip ADR-0025 §8, PLG-028). A process the plugin starts, such as a
// model process under an executor, must not inherit it: it could then
// call the host as the plugin. Serve already moves the channel to private
// close-on-exec descriptors (os.Stdin and os.Stdout then name the null
// device and stderr), which covers any exec.Cmd on Unix. Command is the
// safe way to start children everywhere, including under ServeIO with the
// process's own streams and on platforms without that isolation.

// Command returns an exec.Cmd for name and args that does not inherit the
// channel: its stdin and stdout are the null device unless the caller sets
// them to something else, its stderr is the plugin's stderr (diagnostics
// are free-form), and it has no extra files. Go marks every other
// descriptor close-on-exec. Call CheckChild before starting a Cmd built
// another way, or after changing its streams.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	return cmd
}

// ErrChildInheritsChannel is returned by CheckChild.
var ErrChildInheritsChannel = errors.New("gipplugin: a child process must not inherit the plugin's JSON-RPC channel (stdin or stdout)")

// CheckChild reports ErrChildInheritsChannel when cmd names os.Stdin or
// os.Stdout itself as its Stdin, Stdout or Stderr, or among its
// ExtraFiles. That is all it detects: a wrapped writer such as
// io.MultiWriter(os.Stdout, buf), or a file the plugin made with
// os.NewFile(1, ...), passes. Under Serve on Unix those are harmless (the
// channel is no longer on descriptors 0 and 1); elsewhere use Command.
func CheckChild(cmd *exec.Cmd) error {
	channel := func(v any) bool {
		f, ok := v.(*os.File)
		return ok && f != nil && (f == os.Stdin || f == os.Stdout)
	}
	if channel(cmd.Stdin) || channel(cmd.Stdout) || channel(cmd.Stderr) {
		return ErrChildInheritsChannel
	}
	for _, f := range cmd.ExtraFiles {
		if channel(f) {
			return ErrChildInheritsChannel
		}
	}
	return nil
}
