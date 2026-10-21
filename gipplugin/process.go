package gipplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// The JSON-RPC channel (the plugin's stdin and stdout) is the only way to
// the host, and so to the plugin's data (gip ADR-0025 §8, PLG-028). A
// process the plugin starts, such as a model process under an executor,
// must not inherit it: it could then call the host as the plugin. gip
// cannot enforce what a plugin does with its own descriptors, so the SDK
// gives a helper that starts children without them.

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

// CheckChild reports ErrChildInheritsChannel when cmd would hand the
// plugin's stdin or stdout to the child: as its Stdin, Stdout or Stderr,
// or among its ExtraFiles (compared as os.Stdin and os.Stdout; a file
// the plugin opened on descriptor 0 or 1 itself is its own business). Use
// it on every exec.Cmd a plugin starts.
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
