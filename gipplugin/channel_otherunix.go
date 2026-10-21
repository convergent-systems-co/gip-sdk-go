//go:build unix && !linux

package gipplugin

import "syscall"

func dup2(oldfd, newfd int) error { return syscall.Dup2(oldfd, newfd) }

// hardenProcess has nothing to do outside Linux: there is no /proc to
// reopen descriptors through (macOS task ports need entitlements a
// same-user child does not hold).
func hardenProcess() error { return nil }
