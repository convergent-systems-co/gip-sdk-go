package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestV0121PlanRulesRoundTrip(t *testing.T) {
	b, _ := json.Marshal(PlanDocument{Rules: 2})
	for _, w := range []string{`"rules":2`} {
		if !strings.Contains(string(b), w) {
			t.Fatalf("%s lacks %s", b, w)
		}
	}
	var back PlanDocument
	if err := json.Unmarshal(b, &back); err != nil || back.Rules != 2 {
		t.Fatalf("%+v %v", back, err)
	}
	b, _ = json.Marshal(PlanDocument{})
	if strings.Contains(string(b), `"rules"`) {
		t.Fatalf("zero values must be omitted: %s", b)
	}
}

func TestV0121OutsideInvariants(t *testing.T) {
	p := HostActParams{Kind: ActReview, Work: "w", As: "a", Outcome: ReviewReject, OutsideInvariants: []InvariantRef{{ID: "inv-1", Revision: 3}, {ID: "inv-2"}}}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"outside_invariants":[{"id":"inv-1","revision":3},{"id":"inv-2"}]`) {
		t.Fatalf("%s", b)
	}
	var back HostActParams
	if err := json.Unmarshal(b, &back); err != nil || len(back.OutsideInvariants) != 2 || back.OutsideInvariants[0].Revision != 3 {
		t.Fatalf("%+v %v", back, err)
	}
	b, _ = json.Marshal(HostActParams{Kind: ActStart, Work: "w", As: "a"})
	if strings.Contains(string(b), "outside_invariants") {
		t.Fatalf("omitempty: %s", b)
	}
}

func TestV0121CoveredBy(t *testing.T) {
	b, _ := json.Marshal(FindingResolution{Finding: "f1", Dismiss: "duplicate", CoveredBy: "A1.2"})
	if !strings.Contains(string(b), `"covered_by":"A1.2"`) {
		t.Fatalf("%s", b)
	}
	var back FindingResolution
	if err := json.Unmarshal(b, &back); err != nil || back.CoveredBy != "A1.2" {
		t.Fatalf("%+v %v", back, err)
	}
	b, _ = json.Marshal(FindingResolution{Finding: "f1", Dismiss: "x"})
	if strings.Contains(string(b), "covered_by") {
		t.Fatalf("omitempty: %s", b)
	}
}
