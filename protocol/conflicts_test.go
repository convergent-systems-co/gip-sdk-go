package protocol

import (
	"slices"
	"strings"
	"testing"
)

// TestConflictMethodsAndPermission pins the #108 surface: three host
// methods gated by one known permission of their own, and the codes core
// already uses for the D4 and D5 refusals.
func TestConflictMethodsAndPermission(t *testing.T) {
	want := []string{"host/conflicts/list", "host/conflicts/resolve", "host/conflicts/decline"}
	if !slices.Equal(ConflictMethods(), want) {
		t.Fatalf("methods %q", ConflictMethods())
	}
	if PermConflictsResolve != "conflicts.resolve" || !slices.Contains(KnownPermissions, PermConflictsResolve) {
		t.Fatal("conflicts.resolve must be a known permission")
	}
	for _, c := range [][2]string{
		{CodeConflictOfInterest, "CONFLICT_OF_INTEREST"},
		{CodeHumanClassConflict, "HUMAN_CLASS_CONFLICT"},
		{CodeAuthorsUnknown, "AUTHORS_UNKNOWN"},
		{CodeQuestionOpen, "QUESTION_OPEN"},
		{CodeStaleRevision, "STALE_REVISION"},
		{CodeConflictNotOpen, "CONFLICT_NOT_OPEN"},
		{CodeConflictRequestInvalid, "CONFLICT_REQUEST_INVALID"},
		{CodeHumanRequired, "HUMAN_REQUIRED"},
		{CodeAuthorityRevoked, "AUTHORITY_REVOKED"},
		{CodeIdentityDowngrade, "IDENTITY_DOWNGRADE"},
		{CodePresenceEnrolled, "PRESENCE_ENROLLED"},
		{CodeRulesRequireSettings, "RULES_REQUIRE_SETTINGS"},
		{CodeIdempotencyConflict, "IDEMPOTENCY_CONFLICT"},
		{DispositionAccepted, "ACCEPTED"},
		{DispositionRejected, "REJECTED"},
		{DispositionBlocked, "BLOCKED"},
		{DispositionHumanRequired, "HUMAN_REQUIRED"},
	} {
		if c[0] != c[1] {
			t.Errorf("code %q drifted from %q", c[0], c[1])
		}
	}
	if MaxConflictRationaleBytes != 4000 {
		t.Fatal("rationale limit")
	}
	m := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-reviewer","version":"0.1.0","protocol_versions":[1],"executable":"x",
	"permissions":["work.read","work.review","conflicts.resolve"],
	"commands":[{"name":"resolve","summary":"s","permissions":["conflicts.resolve"]}]}`
	if _, err := ParseManifest([]byte(m)); err != nil {
		t.Fatalf("a reviewer declares conflicts.resolve: %v", err)
	}
	if _, err := ParseManifest([]byte(strings.Replace(m, `"work.review","conflicts.resolve"],`, `"work.review"],`, 1))); err == nil {
		t.Fatal("a command cannot use conflicts.resolve the plugin does not declare")
	}
}

// TestConflictWireShapes pins the JSON of the conflict methods.
func TestConflictWireShapes(t *testing.T) {
	var l HostConflictsListResult
	strict(t, `{"conflicts":[{"id":"cer-7","revision":1,"object":{"kind":"work","id":"w-3","revision":4},"class":"agent","codes":["DIVERGED"],
	"candidates":[{"id":"a","head":"h1","revision":4,"summary":"s","author":"agent:b","provisional":true,"presence_flagged":true,
	"adoptable":false,"refusal":{"code":"PRESENCE_ENROLLED","reason":"r"},"record":{"id":"w-3"}}],
	"authors":["agent:b"],"question":"amb-1","refusal":{"code":"QUESTION_OPEN","reason":"amb-1"}}]}`, &l)
	c := l.Conflicts[0]
	if c.Object.ID != "w-3" || !c.QuestionOpen() || c.AgentResolvable() || c.Eligible() || c.Refusal.Code != CodeQuestionOpen ||
		!c.Candidates[0].PresenceFlagged || c.Candidates[0].Refusal.Code != CodePresenceEnrolled || string(c.Candidates[0].Record) != `{"id":"w-3"}` {
		t.Fatalf("%+v", c)
	}
	encodes(t, HostConflictsListParams{As: "agent:r"}, []string{`{"as":"agent:r"}`})
	encodes(t, ConflictCandidate{ID: "a"}, []string{`"adoptable":false`}, `"record"`, `"refusal"`)
	var e ConflictCeremony
	strict(t, `{"id":"c","revision":1,"object":{"kind":"work","id":"w","revision":1},"candidates":[]}`, &e)
	if !e.Eligible() {
		t.Fatal("no refusal means eligible")
	}
	encodes(t, HostConflictResolveParams{As: "agent:r", Ceremony: "cer-7", Revision: 1, Adopt: "b", Rationale: "r", RequestID: "q"},
		[]string{`"as":"agent:r"`, `"ceremony":"cer-7"`, `"revision":1`, `"adopt":"b"`, `"rationale":"r"`, `"request_id":"q"`}, `"keep"`)
	encodes(t, HostConflictResolveParams{Keep: true}, []string{`"keep":true`}, `"adopt"`)
	var r HostConflictResolveResult
	strict(t, `{"disposition":"ACCEPTED","reasons":[],"result":"ADOPTED b","event_seq":40}`, &r)
	strict(t, `{"disposition":"REJECTED","code":"CONFLICT_OF_INTEREST","reasons":["author"],"event_seq":0}`, &r)
	encodes(t, HostConflictDeclineParams{As: "agent:r", Ceremony: "cer-7", Revision: 1, Reason: "x"}, []string{`"reason":"x"`}, `"request_id"`)
	var d HostConflictDeclineResult
	strict(t, `{"disposition":"ACCEPTED","reasons":[],"question":"amb-20","raised":true,"event_seq":41}`, &d)
	var mp MergePending
	strict(t, `{"parents":["p"],"provisional":"a","candidates":[],"authors":["agent:b"],"class":"agent","presence_flagged":["b"]}`, &mp)
}

func TestConflictTextValidation(t *testing.T) {
	for _, tc := range []struct {
		s  string
		ok bool
	}{
		{"fine", true},
		{"two\nlines\tand tab", true},
		{"é unicode", true},
		{strings.Repeat("x", MaxConflictRationaleBytes), true},
		{"", false},
		{" \n\t", false},
		{"cr\r", false},
		{"nul\x00", false},
		{"\xff", false},
		{strings.Repeat("x", MaxConflictRationaleBytes+1), false},
	} {
		err := ValidConflictText("rationale", tc.s)
		if (err == nil) != tc.ok {
			t.Errorf("%q: %v, want ok=%v", tc.s, err, tc.ok)
		}
		if err != nil && (ErrorID(err) != CodeConflictRequestInvalid || !isCode(err, CodeInvalidParams)) {
			t.Errorf("%q: %v", tc.s, err)
		}
	}
}
