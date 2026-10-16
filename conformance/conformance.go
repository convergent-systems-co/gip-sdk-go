// Package conformance checks that a plugin binary honours the gip plugin
// protocol. Plugin repositories run it from their own tests:
//
//	report := conformance.Check(ctx, "path/to/plugin-dir")
//	for _, c := range report.Checks { if !c.Pass { t.Error(c.Name, c.Detail) } }
//
// The checks exercise only the protocol surface (handshake, identity,
// version negotiation, error shapes). They never invoke a plugin command,
// because commands may have side effects the harness cannot reason about.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/convergent-systems-co/governed-intent-programming/sdk/go/protocol"
)

// Check is one conformance assertion.
type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// Report is the result of Run.
type Report struct {
	Plugin string  `json:"plugin"`
	Checks []Check `json:"checks"`
}

// Passed reports whether every check passed.
func (r *Report) Passed() bool {
	for _, c := range r.Checks {
		if !c.Pass {
			return false
		}
	}
	return len(r.Checks) > 0
}

// Run checks the plugin installed in dir (the directory holding
// gip-plugin.json).
func Run(ctx context.Context, dir string) *Report {
	r := &Report{}
	add := func(id, name string, err error) {
		c := Check{ID: id, Name: name, Pass: err == nil}
		if err != nil {
			c.Detail = err.Error()
		}
		r.Checks = append(r.Checks, c)
	}

	data, err := os.ReadFile(filepath.Join(dir, protocol.ManifestFile))
	if err != nil {
		add("CNF-001", "manifest is readable", err)
		return r
	}
	m, err := protocol.ParseManifest(data)
	add("CNF-001", "manifest is valid", err)
	if err != nil {
		return r
	}
	r.Plugin = m.Name
	exe := filepath.Join(dir, filepath.FromSlash(m.Executable))
	if _, err := os.Stat(exe); err != nil {
		if _, err2 := os.Stat(exe + ".exe"); err2 == nil {
			exe += ".exe"
		} else {
			add("CNF-002", "executable exists", err)
			return r
		}
	}
	add("CNF-002", "executable exists", nil)

	add("CNF-003", "initialize restates manifest identity", session(ctx, exe, func(c *protocol.Conn) error {
		res, perr, err := call(c, 1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{protocol.Version}, HostName: "conformance"})
		if err != nil {
			return err
		}
		if perr != nil {
			return perr
		}
		var ir protocol.InitializeResult
		if err := json.Unmarshal(res, &ir); err != nil {
			return err
		}
		if ir.Name != m.Name || ir.Version != m.Version || ir.ProtocolVersion != protocol.Version {
			return fmt.Errorf("got %s@%s protocol %d, manifest says %s@%s", ir.Name, ir.Version, ir.ProtocolVersion, m.Name, m.Version)
		}
		return nil
	}))

	add("CNF-004", "unsupported protocol version is refused", session(ctx, exe, func(c *protocol.Conn) error {
		_, perr, err := call(c, 1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{9999}, HostName: "conformance"})
		if err != nil {
			return err
		}
		if perr == nil || perr.Code != protocol.CodeIncompatible {
			return fmt.Errorf("expected error code %d, got %v", protocol.CodeIncompatible, perr)
		}
		return nil
	}))

	add("CNF-005", "invoke before initialize is refused", session(ctx, exe, func(c *protocol.Conn) error {
		leaf := m.Leaves()[0]
		_, perr, err := call(c, 1, protocol.MethodInvoke, protocol.InvokeParams{Command: leaf, Arguments: map[string]any{}})
		if err != nil {
			return err
		}
		if perr == nil {
			return errors.New("plugin executed a command before initialize")
		}
		return nil
	}))

	add("CNF-006", "unknown method returns method-not-found", session(ctx, exe, func(c *protocol.Conn) error {
		_, perr, err := call(c, 1, "no/such-method", struct{}{})
		if err != nil {
			return err
		}
		if perr == nil || perr.Code != protocol.CodeMethodNotFound {
			return fmt.Errorf("expected code %d, got %v", protocol.CodeMethodNotFound, perr)
		}
		return nil
	}))

	add("CNF-007", "unknown command is refused with a stable error id", session(ctx, exe, func(c *protocol.Conn) error {
		if _, perr, err := call(c, 1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{protocol.Version}}); err != nil || perr != nil {
			return errors.Join(err, errOrNil(perr))
		}
		_, perr, err := call(c, 2, protocol.MethodInvoke, protocol.InvokeParams{Command: []string{"no-such-command"}, Arguments: map[string]any{}})
		if err != nil {
			return err
		}
		if perr == nil || perr.Data == nil || perr.Data.ID == "" {
			return fmt.Errorf("expected a structured error with an id, got %v", perr)
		}
		return nil
	}))

	add("CNF-008", "exits after stdin closes", func() error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe)
		cmd.Stdin = eofReader{}
		cmd.Stdout = io.Discard
		return cmd.Run()
	}())
	return r
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

func errOrNil(e *protocol.Error) error {
	if e == nil {
		return nil
	}
	return e
}

func session(ctx context.Context, exe string, fn func(*protocol.Conn) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	ferr := fn(protocol.NewConn(stdout, stdin))
	_ = stdin.Close()
	werr := cmd.Wait()
	if ferr != nil {
		return ferr
	}
	if werr != nil {
		return fmt.Errorf("plugin exited with error: %w", werr)
	}
	return nil
}

// call sends a request and waits for its response, skipping telemetry
// notifications.
func call(c *protocol.Conn, id int64, method string, params any) (json.RawMessage, *protocol.Error, error) {
	msg, err := protocol.Request(id, method, params)
	if err != nil {
		return nil, nil, err
	}
	if err := c.Write(msg); err != nil {
		return nil, nil, err
	}
	for {
		resp, err := c.Read()
		if err != nil {
			return nil, nil, err
		}
		if resp.Method != "" {
			continue
		}
		return resp.Result, resp.Error, nil
	}
}
