//go:build unix

package gipplugin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
)

const childEnv = "GIPPLUGIN_TEST_CHILD"

func fdID(fd int) string {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return "closed"
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}

func pathID(p string) string {
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}

// TestChildrenDoNotInheritTheChannel (PLG-028, ADR-0025 §8): a process
// started with Command gets the null device, not the plugin's stdin and
// stdout, so it cannot speak to the host as the plugin.
func TestChildrenDoNotInheritTheChannel(t *testing.T) {
	if os.Getenv(childEnv) == "1" {
		// The child: report what its descriptors 0 and 1 are.
		fmt.Fprintf(os.Stderr, "%s %s %s", fdID(0), fdID(1), pathID(os.DevNull))
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := Command(context.Background(), exe, "-test.run=^TestChildrenDoNotInheritTheChannel$")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	var report bytes.Buffer
	cmd.Stderr = &report
	if err := CheckChild(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("child: %v (%s)", err, report.String())
	}
	var in, out, null string
	if _, err := fmt.Sscan(report.String(), &in, &out, &null); err != nil {
		t.Fatalf("child report %q: %v", report.String(), err)
	}
	if in != null || out != null {
		t.Fatalf("child stdin %s, stdout %s; want the null device %s", in, out, null)
	}
	// The channel is this process's stdin and stdout: when they are not the
	// null device, the child's must differ from them.
	for fd, got := range map[int]string{0: in, 1: out} {
		if mine := fdID(fd); mine != null && mine == got {
			t.Fatalf("child inherited descriptor %d (%s)", fd, mine)
		}
	}
}
