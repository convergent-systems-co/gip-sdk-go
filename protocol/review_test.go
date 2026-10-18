package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// The review act (SDK v0.6.0): its kind, its parameters on the wire, and
// the work.review permission a reviewer plugin declares.
func TestReviewAct(t *testing.T) {
	if ActReview != "review" || ReviewApprove != "approve" || ReviewReject != "reject" {
		t.Fatal("the review act kind and outcomes are part of the protocol")
	}
	p := HostActParams{Kind: ActReview, Work: "work-w", As: "agent:reviewer", Ceremony: "cer-1", Outcome: ReviewApprove, Rationale: "meets its objective", Evidence: []string{"ev-1"}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"review"`, `"ceremony":"cer-1"`, `"outcome":"approve"`, `"rationale":"meets its objective"`, `"evidence":["ev-1"]`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("review act %s lacks %s", b, want)
		}
	}
	var back HostActParams
	if err := json.Unmarshal(b, &back); err != nil || back.Ceremony != "cer-1" || back.Outcome != ReviewApprove || len(back.Evidence) != 1 {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	// Other acts send none of the review fields.
	b, _ = json.Marshal(HostActParams{Kind: ActStart, Work: "w", As: "agent:x"})
	for _, absent := range []string{"ceremony", "outcome", "evidence"} {
		if strings.Contains(string(b), absent) {
			t.Fatalf("a start act carries no %s: %s", absent, b)
		}
	}

	reviewer := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-reviewer","version":"0.1.0","protocol_versions":[1],"executable":"x",
	"permissions":["work.read","work.review"],
	"commands":[{"name":"review","summary":"s","permissions":["work.read","work.review"]}]}`
	if _, err := ParseManifest([]byte(reviewer)); err != nil {
		t.Fatalf("a reviewer declares work.review: %v", err)
	}
	undeclared := strings.Replace(reviewer, `"permissions":["work.read","work.review"],`, `"permissions":["work.read"],`, 1)
	if _, err := ParseManifest([]byte(undeclared)); err == nil {
		t.Fatal("a command cannot use work.review the plugin does not declare")
	}
}
