package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestLeaseAndProgressMessages pins the wire shape of the lease and
// progress additions (SDK v0.3.0): additive, omitted when unused, and
// round-trippable.
func TestLeaseAndProgressMessages(t *testing.T) {
	// Fields added to existing messages are omitted when unused, so v0.2.0
	// peers see exactly what they saw before.
	b, _ := json.Marshal(HostActParams{Kind: ActStart, Work: "w", As: "agent:x"})
	if strings.Contains(string(b), "lease") || strings.Contains(string(b), "rotate") {
		t.Fatalf("unused lease fields must be omitted: %s", b)
	}
	b, _ = json.Marshal(HostActResult{Disposition: "ACCEPTED", Reasons: []string{}})
	if strings.Contains(string(b), "lease") {
		t.Fatalf("an act result without a lease must not carry one: %s", b)
	}
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	res := HostActResult{Disposition: "ACCEPTED", Reasons: []string{}, Lease: &Lease{ID: "lease-1", Holder: "agent:x", TTLSeconds: 1800, GrantedAt: at, RenewedAt: at, ExpiresAt: at.Add(30 * time.Minute), State: LeaseActive}}
	b, _ = json.Marshal(res)
	var back HostActResult
	if err := json.Unmarshal(b, &back); err != nil || back.Lease == nil || !back.Lease.ExpiresAt.Equal(at.Add(30*time.Minute)) || back.Lease.State != LeaseActive {
		t.Fatalf("lease round trip: %s %+v %v", b, back.Lease, err)
	}
	for _, want := range []string{`"ttl_seconds":1800`, `"expires_at":"2026-10-17T12:30:00Z"`, `"holder":"agent:x"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("act result %s lacks %s", b, want)
		}
	}
	b, _ = json.Marshal(HostActParams{Kind: ActRenew, Work: "w", As: "agent:x", Lease: "lease-1", RotateLease: true})
	if !strings.Contains(string(b), `"rotate_lease":true`) {
		t.Fatalf("rotate_lease: %s", b)
	}
	pct := 40.0
	p := HostProgressParams{Work: "w", As: "agent:x", Lease: "lease-1", Phase: PhaseChecks, Message: "running 2 checks", Percent: &pct,
		Cost: []CostEntry{{Resource: "model.usd", Value: 0.12, Unit: "USD", Basis: BasisReported}}}
	b, _ = json.Marshal(p)
	var pb HostProgressParams
	if err := json.Unmarshal(b, &pb); err != nil || pb.Percent == nil || *pb.Percent != 40 || pb.Cost[0].Resource != "model.usd" {
		t.Fatalf("progress round trip: %s %+v %v", b, pb, err)
	}
	b, _ = json.Marshal(HostProgressParams{Work: "w", As: "agent:x", Phase: PhaseAgent})
	if strings.Contains(string(b), "percent") {
		t.Fatalf("an absent percent is omitted, never sent as 0: %s", b)
	}
	b, _ = json.Marshal(HostExecutionResult{Units: []ExecutionStatus{{Work: "w", Execution: "in_progress", Progress: []ProgressEvent{}}}})
	if !strings.Contains(string(b), `"progress":[]`) {
		t.Fatalf("execution status lists progress even when empty: %s", b)
	}
	for _, k := range []string{ActStart, ActSubmit, ActQualify, ActIntegrate, ActRework, ActRenew, ActReclaim} {
		if k == "" {
			t.Fatal("act kinds are non-empty")
		}
	}
	if MethodHostProgress != "host/progress" || MethodHostExecution != "host/execution" {
		t.Fatal("method names are part of the protocol")
	}
}
