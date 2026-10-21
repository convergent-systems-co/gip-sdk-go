//go:build unix

package gipplugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

const modeEnv = "GIPPLUGIN_TEST_MODE"

func TestMain(m *testing.M) {
	switch os.Getenv(modeEnv) {
	case "serve":
		os.Exit(serveHelper())
	case "junk":
		// A child that tries to use the channel: write to descriptors 1 and
		// 3, and read stdin to the end.
		for _, fd := range []int{1, 3} {
			_, _ = syscall.Write(fd, []byte("JUNK-LINE\n"))
		}
		n, err := io.ReadAll(os.Stdin)
		fmt.Fprintf(os.Stderr, "JUNK-READ %d %v\n", len(n), err)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// serveHelper is a plugin under Serve whose command prints to stdout and
// starts children handed the channel in the ways CheckChild cannot see.
func serveHelper() int {
	h := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		fmt.Println("STRAY-PRINT")
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		var copyBuf bytes.Buffer
		c1 := exec.Command(exe)
		c1.Env = append(os.Environ(), modeEnv+"=junk")
		c1.Stdin = os.Stdin
		c1.Stdout = io.MultiWriter(os.Stdout, &copyBuf)
		c1.Stderr = os.Stderr
		if err := c1.Run(); err != nil {
			return nil, err
		}
		c2 := exec.Command(exe)
		c2.Env = append(os.Environ(), modeEnv+"=junk")
		c2.ExtraFiles = []*os.File{os.NewFile(1, "fd1")}
		c2.Stderr = os.Stderr
		if err := c2.Run(); err != nil {
			return nil, err
		}
		return &protocol.InvokeResult{Text: "ok"}, nil
	}
	return Serve([]byte(testManifest), Handlers{"t run": h})
}

// TestServeIsolatesTheChannel (ADR-0025 §8): under Serve, a stray print
// and children handed os.Stdin, io.MultiWriter(os.Stdout, ...) or
// os.NewFile(1, ...) neither corrupt the protocol nor reach the channel.
func TestServeIsolatesTheChannel(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = append(os.Environ(), modeEnv+"=serve")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	conn := protocol.NewConn(stdout, stdin)
	init, _ := protocol.Request(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{1}, GrantedPermissions: []string{"work.read", "work.execute"}})
	inv, _ := protocol.Request(2, protocol.MethodInvoke, protocol.InvokeParams{Command: []string{"t", "run"}, Arguments: map[string]any{}})
	for _, m := range []*protocol.Message{init, inv} {
		if err := conn.Write(m); err != nil {
			t.Fatal(err)
		}
		resp, err := conn.Read()
		if err != nil {
			t.Fatalf("protocol corrupted: %v (stderr %q)", err, stderr.String())
		}
		if resp.Error != nil || string(*resp.ID) != string(*m.ID) {
			t.Fatalf("response %+v (stderr %q)", resp, stderr.String())
		}
	}
	_ = stdin.Close()
	if m, err := conn.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the last response the channel carried %+v %v", m, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("plugin: %v (stderr %q)", err, stderr.String())
	}
	got := stderr.String()
	if !strings.Contains(got, "STRAY-PRINT") || strings.Count(got, "JUNK-LINE") < 2 || strings.Count(got, "JUNK-READ 0 <nil>") != 2 {
		t.Fatalf("stray output and children must land on stderr and read the null device: %q", got)
	}
}
