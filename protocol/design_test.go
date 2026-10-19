package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPlanDesign pins the front-loaded planning additions (SDK v0.7.0):
// a pass may carry a design, the plan document and the work contract
// expose it, and the document says when the delivery's final discovery
// ran. Everything is additive: older messages encode as before.
func TestPlanDesign(t *testing.T) {
	if MaxDesignBytes != 65536 {
		t.Fatalf("a design is at most 64 KiB: %d", MaxDesignBytes)
	}
	b, _ := json.Marshal(PlanPass{Trigger: TriggerInitial, Scope: "A", Strategy: "s"})
	if strings.Contains(string(b), "design") {
		t.Fatalf("a pass without a design omits it: %s", b)
	}
	b, _ = json.Marshal(PlanPass{Trigger: TriggerInitial, Scope: "A", Strategy: "s", Design: "# Architecture\n"})
	if !strings.Contains(string(b), `"design":"# Architecture\n"`) {
		t.Fatalf("a pass carries its design: %s", b)
	}
	doc := `{"schema":"gip.plan-document/v1","algorithm":"gip.plan/1","project_id":"p","state_head":9,"pass":3,"planned":true,"root":"A",
	"evaluated_at":"2026-10-18T12:00:00Z","ready":[],"in_flight":[],"counts":{},"milestones":[],"triggers":[],"findings":[],"unplanned":[],"nodes":[],
	"design":{"revision":2,"pass":3,"digest":"sha256:ab","content":"# Design","planners":[{"planner":"p1","model":"m1"}]},
	"delivery":{"pass":3,"findings":["finding-1"]},"digest":"sha256:d"}`
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.DisallowUnknownFields()
	var d PlanDocument
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("plan document: %v", err)
	}
	if d.Design == nil || d.Design.Revision != 2 || d.Design.Pass != 3 || d.Design.Content != "# Design" || d.Design.Planners[0].Model != "m1" {
		t.Fatalf("design %+v", d.Design)
	}
	if d.Delivery == nil || d.Delivery.Pass != 3 || d.Delivery.Findings[0] != "finding-1" {
		t.Fatalf("delivery %+v", d.Delivery)
	}
	b, _ = json.Marshal(PlanDocument{})
	if strings.Contains(string(b), "design") || strings.Contains(string(b), "delivery") {
		t.Fatalf("a document without a design or delivery omits them: %s", b)
	}

	r := HostContractResult{Contract: json.RawMessage(`{"schema":"gip.work-contract/v1","design":{"revision":1,"pass":1,"digest":"sha256:x","content":"routes"}}`)}
	if x, err := r.Design(); err != nil || x == nil || x.Content != "routes" || x.Revision != 1 {
		t.Fatalf("contract design %+v (%v)", x, err)
	}
	for _, c := range []string{`{"schema":"gip.work-contract/v1"}`, ``} {
		if x, err := (&HostContractResult{Contract: json.RawMessage(c)}).Design(); err != nil || x != nil {
			t.Fatalf("a contract without a design has none: %+v (%v)", x, err)
		}
	}
	if _, err := (&HostContractResult{Contract: json.RawMessage(`{`)}).Design(); err == nil {
		t.Fatal("a malformed contract is an error")
	}
}

// TestContractDesign pins the contract design's pin fields (SDK v0.8.0):
// the served revision, whether it is the unit's pin, and the plan's
// current revision. Older contracts decode with the fields zero.
func TestContractDesign(t *testing.T) {
	r := HostContractResult{Contract: json.RawMessage(`{"schema":"gip.work-contract/v1","design":{"revision":2,"pass":3,"digest":"sha256:a","content":"routes","planners":[{"planner":"p","model":"m"}],` +
		`"pinned":true,"current_revision":3,"current_digest":"sha256:b"}}`)}
	d, err := r.Design()
	if err != nil || d == nil {
		t.Fatalf("contract design %+v (%v)", d, err)
	}
	if d.Content != "routes" || d.Revision != 2 || d.Pass != 3 || d.Digest != "sha256:a" || d.Planners[0].Model != "m" {
		t.Fatalf("embedded plan design %+v", d.PlanDesign)
	}
	if !d.Pinned || d.CurrentRevision != 3 || d.CurrentDigest != "sha256:b" {
		t.Fatalf("pin fields %+v", d)
	}
	if !d.Superseded() {
		t.Fatal("a pinned design whose digest differs from the current one is superseded")
	}
	if p := d.Pin(); p == nil || *p != (DesignPin{Revision: 2, Digest: "sha256:a"}) {
		t.Fatalf("pin %+v", p)
	}

	// Same revision number, different text (a ledger merge): the digest decides.
	d.CurrentRevision, d.CurrentDigest = 2, "sha256:c"
	if !d.Superseded() {
		t.Fatal("the digest, not the revision number, decides supersession")
	}
	d.CurrentDigest = "sha256:a"
	if d.Superseded() {
		t.Fatal("the current design is not superseded")
	}

	// A gip that predates pinning: no pin fields, never superseded.
	old := HostContractResult{Contract: json.RawMessage(`{"design":{"revision":1,"pass":1,"digest":"sha256:x","content":"c"}}`)}
	d, err = old.Design()
	if err != nil || d == nil || d.Pinned || d.CurrentRevision != 0 || d.CurrentDigest != "" || d.Superseded() {
		t.Fatalf("pre-pinning contract design %+v (%v)", d, err)
	}

	// A contract without a design: nil, and the methods are nil-safe.
	var none *ContractDesign
	if none.Superseded() || none.Pin() != nil {
		t.Fatal("a nil design is not superseded and has no pin")
	}
	if b, _ := json.Marshal(ContractDesign{}); strings.Contains(string(b), "pinned") || strings.Contains(string(b), "current_") {
		t.Fatalf("unpinned fields are omitted: %s", b)
	}

	// Round trip: the fields encode flat, beside the plan design's.
	b, _ := json.Marshal(ContractDesign{PlanDesign: PlanDesign{Revision: 1, Pass: 1, Digest: "sha256:x", Content: "c"}, Pinned: true, CurrentRevision: 1, CurrentDigest: "sha256:x"})
	for _, f := range []string{`"revision":1`, `"content":"c"`, `"pinned":true`, `"current_revision":1`, `"current_digest":"sha256:x"`} {
		if !strings.Contains(string(b), f) {
			t.Fatalf("encoded contract design lacks %s: %s", f, b)
		}
	}
}
