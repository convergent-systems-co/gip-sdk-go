//go:build !unix

package gipplugin

import "os"

// isolateChannel leaves the standard streams as they are on platforms
// without POSIX descriptors; use Command for child processes there.
func isolateChannel() (in, out *os.File, err error) { return os.Stdin, os.Stdout, nil }
