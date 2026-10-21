package protocol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestConflictQuestionClosesByLedgerResolve(t *testing.T) {
	var q PlanQuestion
	raw := `{"id":"amb-9","text":"merge-conflict: grant-4 has two versions","class":"decision","blocking":true,
"refs":["ceremony-12","merge-conflict"],"status":"answered","closed_by":{"kind":"ledger.resolve","ref":"ev-abc"}}`
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatal(err)
	}
	if !q.ClosedBy.IsClosedByLedgerResolve() || q.ClosedBy.Ref != "ev-abc" || !slices.Contains(q.Refs, QuestionTagMergeConflict) {
		t.Fatalf("closure %+v", q.ClosedBy)
	}
	if (&QuestionClosure{Kind: ClosedByRevision}).IsClosedByLedgerResolve() || (*QuestionClosure)(nil).IsClosedByLedgerResolve() {
		t.Fatal("only kind ledger.resolve is a ledger resolution")
	}
	if !slices.Equal(ReservedQuestionTags(), []string{"unreviewable-exhausted", "rescope-exhausted", "merge-conflict"}) {
		t.Fatalf("reserved tags %q", ReservedQuestionTags())
	}
}

func TestAttestationPresence(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	a := Attestation{AttestedBy: "interactive", Shown: "sha256:s", Basis: "sha256:b", Presence: &Presence{Key: "p256:abc", SignedAt: at, Sig: "c2ln"}}
	b, _ := json.Marshal(a)
	want := `{"attested_by":"interactive","shown":"sha256:s","basis":"sha256:b","presence":{"key":"p256:abc","signed_at":"2026-10-21T09:00:00Z","sig":"c2ln"}}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(Attestation{AttestedBy: "interactive"})
	if strings.Contains(string(b), "presence") {
		t.Fatalf("an attestation without presence must not send it: %s", b)
	}
	if PresenceContext != "gip/v1/presence" || PresenceDomainLedger == PresenceDomainInstallation || PresenceFreshness != 5*time.Minute {
		t.Fatal("presence constants drifted from ADR-0025 §5")
	}
}

func TestMergePendingAuthorsAndClass(t *testing.T) {
	var m MergePending
	raw := `{"parents":["a","b"],"provisional":"a","candidates":[{"label":"a","head":"h1","record":{"id":"grant-4"}}],"authors":["agent:builder","agent:ops"],"class":"agent"}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if !m.AuthorsKnown() || !m.IsAuthor("agent:builder") || m.IsAuthor("agent:reviewer") || !m.AgentResolvable() {
		t.Fatalf("pending %+v", m)
	}
	m.Class = ConflictClassHuman
	if m.AgentResolvable() {
		t.Fatal("a human-class conflict is never agent-resolvable")
	}
	var old MergePending
	_ = json.Unmarshal([]byte(`{"parents":["a","b"],"provisional":"a","candidates":[]}`), &old)
	if old.AuthorsKnown() || old.AgentResolvable() {
		t.Fatal("a conflict recorded before rules 3 has no authors (AUTHORS_UNKNOWN)")
	}
	b, _ := json.Marshal(old)
	if strings.Contains(string(b), "authors") || strings.Contains(string(b), "class") {
		t.Fatalf("absent authors and class must stay absent: %s", b)
	}
	if (*MergePending)(nil).AgentResolvable() {
		t.Fatal("nil")
	}
}

func TestHumanOnlyActionsByEpoch(t *testing.T) {
	if !slices.Equal(HumanOnlyActions(RulesAgentAuthority), []string{"intent.revise", "delivery.accept", "project.settings"}) {
		t.Fatalf("rules 3: %q", HumanOnlyActions(RulesAgentAuthority))
	}
	// ADR-0025 §1: the intersection of every epoch's list is
	// {project.settings}.
	var always []string
	for _, a := range HumanOnlyActions(RulesClassic) {
		if slices.Contains(HumanOnlyActions(RulesDarkFactory), a) && slices.Contains(HumanOnlyActions(RulesAgentAuthority), a) {
			always = append(always, a)
		}
	}
	if !slices.Equal(always, []string{"project.settings"}) {
		t.Fatalf("intersection %q", always)
	}
	if HumanOnlyActions(4) != nil {
		t.Fatal("unknown epoch")
	}
}
