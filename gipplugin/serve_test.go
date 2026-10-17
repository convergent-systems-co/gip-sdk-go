package gipplugin

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

const testManifest = `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-t","version":"0.1.0","protocol_versions":[1],"executable":"t",
"permissions":["work.read","work.execute"],
"commands":[{"name":"t","summary":"s","subcommands":[{"name":"run","summary":"s","permissions":["work.read","work.execute"]}]}]}`

// TestProgressAndExecutionCallTheHost drives a plugin through ServeIO with
// a scripted host: Progress and Execution send the v0.3.0 host methods
// with the given parameters and decode the host's answers.
func TestProgressAndExecutionCallTheHost(t *testing.T) {
	hostIn, pluginOut := io.Pipe()
	pluginIn, hostOut := io.Pipe()
	var got []string
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		pct := 50.0
		pr, err := r.Progress(ctx, protocol.HostProgressParams{Work: "w", As: "agent:x", Lease: "lease-1", Phase: protocol.PhaseAgent, Percent: &pct})
		if err != nil {
			return nil, err
		}
		got = append(got, "progress recorded="+map[bool]string{true: "yes", false: "no"}[pr.Recorded]+" code="+pr.Code)
		ex, err := r.Execution(ctx, "w")
		if err != nil {
			return nil, err
		}
		got = append(got, "units="+ex.Units[0].Work+" lease="+ex.Units[0].Lease.ID+" phase="+ex.Units[0].Progress[0].Phase)
		return &protocol.InvokeResult{Text: "ok"}, nil
	}
	done := make(chan int, 1)
	go func() {
		done <- ServeIO(context.Background(), []byte(testManifest), Handlers{"t run": handler}, pluginIn, pluginOut)
	}()
	host := protocol.NewConn(hostIn, hostOut)
	send := func(m *protocol.Message) {
		if err := host.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	init, _ := protocol.Request(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{1}, GrantedPermissions: []string{"work.read", "work.execute"}})
	send(init)
	if m, err := host.Read(); err != nil || m.Error != nil {
		t.Fatalf("initialize: %+v %v", m, err)
	}
	inv, _ := protocol.Request(2, protocol.MethodInvoke, protocol.InvokeParams{Command: []string{"t", "run"}, Arguments: map[string]any{}})
	send(inv)
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for {
		m, err := host.Read()
		if err != nil {
			t.Fatal(err)
		}
		switch m.Method {
		case protocol.MethodHostProgress:
			var p protocol.HostProgressParams
			if err := json.Unmarshal(m.Params, &p); err != nil || p.Lease != "lease-1" || p.Phase != protocol.PhaseAgent || p.Percent == nil || *p.Percent != 50 {
				t.Fatalf("progress params: %s %v", m.Params, err)
			}
			resp, _ := protocol.Response(m.ID, protocol.HostProgressResult{Recorded: false, Code: "LEASE_SUPERSEDED"})
			send(resp)
		case protocol.MethodHostExecution:
			var p protocol.HostExecutionParams
			if err := json.Unmarshal(m.Params, &p); err != nil || p.Work != "w" {
				t.Fatalf("execution params: %s %v", m.Params, err)
			}
			resp, _ := protocol.Response(m.ID, protocol.HostExecutionResult{EvaluatedAt: at, Units: []protocol.ExecutionStatus{{Work: "w", Execution: "in_progress", Executor: "agent:x",
				Lease:    &protocol.Lease{ID: "lease-1", Holder: "agent:x", ExpiresAt: at.Add(time.Minute), State: protocol.LeaseActive},
				Progress: []protocol.ProgressEvent{{At: at, Work: "w", Phase: protocol.PhaseAgent}}}}})
			send(resp)
		case "":
			if m.Error != nil {
				t.Fatalf("invoke failed: %v", m.Error)
			}
			want := []string{"progress recorded=no code=LEASE_SUPERSEDED", "units=w lease=lease-1 phase=agent"}
			if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
				t.Fatalf("got %q want %q", got, want)
			}
			sd, _ := protocol.Notification(protocol.MethodShutdown, struct{}{})
			send(sd)
			if code := <-done; code != 0 {
				t.Fatalf("exit %d", code)
			}
			return
		}
	}
}
