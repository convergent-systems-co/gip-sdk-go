package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPlanMessages pins the plan-once additions (SDK v0.4.0): new methods
// and a new permission, existing messages untouched.
func TestPlanMessages(t *testing.T) {
	for _, m := range []string{MethodHostPlan, MethodHostFinding, MethodHostPlanPass} {
		if !strings.HasPrefix(m, "host/") {
			t.Fatalf("%s is a host method", m)
		}
	}
	found := false
	for _, p := range KnownPermissions {
		found = found || p == PermPlanWrite
	}
	if !found {
		t.Fatal("plan.write is a known permission")
	}
	found = false
	for _, c := range KnownCapabilities {
		found = found || c == CapPlanWork
	}
	if !found {
		t.Fatal("plan.work is a known capability")
	}
	b, _ := json.Marshal(HostPlanParams{})
	if string(b) != "{}" {
		t.Fatalf("an empty plan request is {}: %s", b)
	}
	zero, three := 0, 3
	for _, c := range []struct {
		p    HostPlanParams
		want string
	}{{HostPlanParams{IdleSlots: &zero}, `{"idle_slots":0}`}, {HostPlanParams{IfDigest: "d", IdleSlots: &three}, `{"if_digest":"d","idle_slots":3}`}} {
		if b, _ := json.Marshal(c.p); string(b) != c.want {
			t.Fatalf("an idle report is explicit, 0 included: %s, want %s", b, c.want)
		}
	}
	b, _ = json.Marshal(HostPlanResult{Unchanged: true, Digest: "sha256:x", StateHead: 7})
	if strings.Contains(string(b), `"plan"`) {
		t.Fatalf("an unchanged result carries no document: %s", b)
	}
	doc := `{"schema":"gip.plan-document/v1","algorithm":"gip.plan/1","project_id":"p","state_head":9,"pass":2,"planned":true,"root":"A",
	"evaluated_at":"2026-10-17T12:00:00Z","ready":["B1"],"in_flight":[],"counts":{"ready":1},"milestones":[{"branch":"B","status":"active","remaining":1,"findings":0,"epoch":8,"planned":false}],
	"triggers":[{"kind":"blocking","scope":"B","reason":"r","findings":["finding-1"]}],"findings":[],"unplanned":[],
	"nodes":[{"address":"B1","parent":"B","branch":"B","depth":2,"label":"x","kind":"wu","ref":"w","pass":1,"lifecycle":"live","status":"ready","reasons":[],"rank":1,
	"depends_on":[{"to":"C1","origin":"plan","cross_branch":true,"met":true}],"children":[],"findings":[],"work":{"execution":"not_started","frontier_status":"READY","next_action":"work.start","reasons":[]}}],"digest":"sha256:d"}`
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.DisallowUnknownFields()
	var d PlanDocument
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("plan document: %v", err)
	}
	if n, ok := d.Node("B1"); !ok || n.Work.NextAction != "work.start" || n.Kind != KindWU || !n.DependsOn[0].Cross || d.Triggers[0].Kind != TriggerBlocking {
		t.Fatalf("decoded %+v", d)
	}
	b, _ = json.Marshal(HostPlanPassParams{As: "agent:planner", Pass: PlanPass{Trigger: TriggerMilestone, Scope: "B", Strategy: "s", ExpectedPass: 2,
		Nodes: []PlanNodeFields{{Ref: "n", Parent: "B", Work: &PlanWorkUnit{Objective: "o"}}}}})
	for _, want := range []string{`"expected_pass":2`, `"ref":"n"`, `"work":{"objective":"o"}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("plan pass %s lacks %s", b, want)
		}
	}
	b, _ = json.Marshal(HostFindingParams{Work: "w", As: "agent:x", Title: "t"})
	if strings.Contains(string(b), "blocking") || strings.Contains(string(b), "node") {
		t.Fatalf("unused finding fields are omitted: %s", b)
	}
}
