package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

const reviewerManifest = `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-discovery","version":"0.1.0","protocol_versions":[1],"executable":"x",
"capabilities":["review.intent"],"permissions":["intent.read"],
"commands":[{"name":"planner","summary":"s","subcommands":[{"name":"review","summary":"review the intent","role":"intent.review","timeout_seconds":600,
"arguments":[{"name":"request","type":"string","required":true},{"name":"verbose","type":"boolean"}]}]}]}`

func TestIntentReviewIdentifiers(t *testing.T) {
	if CapReviewIntent != "review.intent" || RoleIntentReview != "intent.review" || IntentReviewRequestArg != "request" ||
		IntentReviewRef != "intent-review" || IntentReviewKindRefPrefix != "kind:" {
		t.Fatal("the intent review identifiers are part of the protocol")
	}
	if got := IntentReviewKinds(); strings.Join(got, ",") != "contradiction,gap,ambiguity,conflict" {
		t.Fatal(got)
	}
	if !contains(KnownCapabilities, CapReviewIntent) || !contains(KnownCommandRoles, RoleIntentReview) || RoleCapability(RoleIntentReview) != CapReviewIntent {
		t.Fatal("review.intent is known and backs intent.review")
	}
}

func TestIntentReviewManifest(t *testing.T) {
	m, err := ParseManifest([]byte(reviewerManifest))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.CommandForRole(RoleIntentReview); strings.Join(got, " ") != "planner review" {
		t.Fatalf("CommandForRole: %v", got)
	}
	if m.CommandForRole("nope") != nil {
		t.Fatal("no command for an unknown role")
	}
	bad := map[string]string{
		"unknown role":            strings.Replace(reviewerManifest, `"role":"intent.review"`, `"role":"intent.bless"`, 1),
		"role without capability": strings.Replace(reviewerManifest, `"capabilities":["review.intent"],`, ``, 1),
		"capability without role": strings.Replace(reviewerManifest, `"role":"intent.review",`, ``, 1),
		"missing request":         strings.Replace(reviewerManifest, `{"name":"request","type":"string","required":true},`, ``, 1),
		"optional request":        strings.Replace(reviewerManifest, `"type":"string","required":true`, `"type":"string"`, 1),
		"integer request":         strings.Replace(reviewerManifest, `"name":"request","type":"string"`, `"name":"request","type":"integer"`, 1),
		"extra required":          strings.Replace(reviewerManifest, `{"name":"verbose","type":"boolean"}`, `{"name":"depth","type":"integer","required":true}`, 1),
		"role on a group":         strings.Replace(reviewerManifest, `{"name":"planner","summary":"s",`, `{"name":"planner","summary":"s","role":"intent.review",`, 1),
		"two commands":            strings.Replace(reviewerManifest, `]}]}]}`, `]},{"name":"again","summary":"s","role":"intent.review","arguments":[{"name":"request","type":"string","required":true}]}]}]}`, 1),
	}
	for name, doc := range bad {
		if _, err := ParseManifest([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A plain plugin with no role is unaffected.
	plain := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-x","version":"0.1.0","protocol_versions":[1],"executable":"x","commands":[{"name":"go","summary":"s"}]}`
	if _, err := ParseManifest([]byte(plain)); err != nil {
		t.Fatal(err)
	}
}

const reviewContent = "Build a CLI that stores notes locally.\nNotes must sync to the cloud in real time.\nUsers are teams."

func sampleRequest() *IntentReviewRequest {
	return &IntentReviewRequest{
		Intent: "main", Revision: 3, Content: reviewContent,
		OpenQuestions:     []PlanQuestion{{ID: "q-1", Text: "Which platforms?", Refs: []string{IntentReviewRef, "kind:gap"}}},
		DeferredQuestions: []PlanQuestion{{ID: "q-2", Text: "Licence?"}},
		Goals:             []StateGoal{{ID: "goal-1", Title: "Notes", Outcome: "notes exist", Status: "admitted"}},
		Invariants:        []StateInvariant{{ID: "inv-1"}},
		RepoSummary:       "Go module, no network code",
		MaxQuestions:      5, Round: 2,
	}
}

func sampleFinding() IntentReviewFinding {
	return IntentReviewFinding{
		Kind:        IntentReviewContradiction,
		Spans:       []string{"stores notes locally.", "Notes must sync  to the cloud in real time."},
		Question:    "Local-only storage or real-time cloud sync?",
		Options:     []string{"local only", "cloud sync", "local first, sync later"},
		Recommended: "local first, sync later",
		Blocking:    true, Rank: 1,
		Rationale: "The two statements choose different architectures.",
	}
}

func TestIntentReviewRequestRoundTrip(t *testing.T) {
	req := sampleRequest()
	args, err := req.Arguments()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateArgs([]Argument{{Name: "request", Type: ArgString, Required: true}}, args); err != nil {
		t.Fatalf("the arguments fit the role's declared argument: %v", err)
	}
	s := args[IntentReviewRequestArg].(string)
	for _, w := range []string{`"intent":"main"`, `"revision":3`, `"content":"Build a CLI`, `"open_questions":[{"id":"q-1"`, `"deferred_questions":[{"id":"q-2"`, `"goals":[{"id":"goal-1"`, `"invariants":[{"id":"inv-1"`, `"repo_summary":"Go module`, `"max_questions":5`, `"round":2`} {
		if !strings.Contains(s, w) {
			t.Fatalf("%s lacks %s", s, w)
		}
	}
	back, err := DecodeIntentReviewRequest(args)
	if err != nil || back.Revision != 3 || len(back.OpenQuestions) != 1 || back.OpenQuestions[0].Refs[1] != "kind:gap" || back.Round != 2 {
		t.Fatalf("%+v %v", back, err)
	}
	min, _ := json.Marshal(IntentReviewRequest{Intent: "main", Revision: 1, Content: "x", MaxQuestions: 1, Round: 1})
	for _, absent := range []string{"open_questions", "deferred_questions", "answered_questions", "goals", "invariants", "repo_summary"} {
		if strings.Contains(string(min), absent) {
			t.Fatalf("omitempty %s: %s", absent, min)
		}
	}
	for name, bad := range map[string]func(*IntentReviewRequest){
		"no intent":     func(r *IntentReviewRequest) { r.Intent = "" },
		"revision 0":    func(r *IntentReviewRequest) { r.Revision = 0 },
		"no content":    func(r *IntentReviewRequest) { r.Content = " " },
		"huge content":  func(r *IntentReviewRequest) { r.Content = strings.Repeat("a", MaxIntentReviewContentBytes+1) },
		"max 0":         func(r *IntentReviewRequest) { r.MaxQuestions = 0 },
		"max too large": func(r *IntentReviewRequest) { r.MaxQuestions = MaxIntentReviewQuestions + 1 },
		"round 0":       func(r *IntentReviewRequest) { r.Round = 0 },
	} {
		r := sampleRequest()
		bad(r)
		if r.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := r.Arguments(); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
	if _, err := DecodeIntentReviewRequest(map[string]any{"request": 7}); err == nil {
		t.Fatal("a non-string request is refused")
	}
	if _, err := DecodeIntentReviewRequest(map[string]any{"request": "{"}); err == nil {
		t.Fatal("malformed JSON is refused")
	}
}

func TestIntentReviewResultRoundTrip(t *testing.T) {
	gap := IntentReviewFinding{Kind: IntentReviewGap, Spans: []string{"Users are teams."}, Question: "How do team members authenticate?", Recommended: "SSO via the team's identity provider", Rank: 2}
	res := IntentReviewResult{Findings: []IntentReviewFinding{sampleFinding(), gap}, Reviewers: []PlanSource{{Planner: "discovery", Model: "claude-opus-5-5"}}}
	if err := res.Validate(sampleRequest()); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	for _, w := range []string{`"kind":"contradiction"`, `"spans":["stores notes locally."`, `"question":"Local-only`, `"options":["local only"`, `"recommended":"local first, sync later"`, `"blocking":true`, `"rank":1`, `"rationale":"The two`, `"reviewers":[{"planner":"discovery","model":"claude-opus-5-5"}]`} {
		if !strings.Contains(string(b), w) {
			t.Fatalf("%s lacks %s", b, w)
		}
	}
	var back IntentReviewResult
	if err := json.Unmarshal(b, &back); err != nil || len(back.Findings) != 2 || back.Findings[1].Blocking || back.Findings[0].Spans[1] != "Notes must sync  to the cloud in real time." {
		t.Fatalf("%+v %v", back, err)
	}
	empty, _ := json.Marshal(IntentReviewResult{Findings: []IntentReviewFinding{}})
	if string(empty) != `{"findings":[]}` {
		t.Fatalf("nothing found: %s", empty)
	}
	if (&IntentReviewResult{}).Validate(sampleRequest()) == nil {
		t.Fatal("findings must be present")
	}
	dupRank := IntentReviewResult{Findings: []IntentReviewFinding{sampleFinding(), sampleFinding()}}
	if dupRank.Validate(sampleRequest()) == nil {
		t.Fatal("ranks are unique")
	}
	req := sampleRequest()
	req.MaxQuestions = 1
	if res.Validate(req) == nil {
		t.Fatal("findings are capped by max_questions")
	}

	q := gap.AsQuestion()
	if q.Class != QuestionClassIntent || q.Text != gap.Question || q.Span != "Users are teams." || q.Blocking || q.Default != gap.Recommended || q.Recommended != gap.Recommended ||
		strings.Join(q.Refs, ",") != "intent-review,kind:gap" {
		t.Fatalf("non-blocking finding as question: %+v", q)
	}
	f := sampleFinding()
	if q := f.AsQuestion(); !q.Blocking || q.Default != "" || len(q.Options) != 3 || q.Refs[1] != "kind:contradiction" {
		t.Fatalf("blocking finding as question: %+v", q)
	}
}

func TestIntentReviewFindingValidation(t *testing.T) {
	for _, k := range IntentReviewKinds() {
		f := sampleFinding()
		f.Kind = k
		if err := f.Validate(reviewContent); err != nil {
			t.Errorf("kind %s: %v", k, err)
		}
	}
	for name, bad := range map[string]func(*IntentReviewFinding){
		"unknown kind":               func(f *IntentReviewFinding) { f.Kind = "vagueness" },
		"empty kind":                 func(f *IntentReviewFinding) { f.Kind = "" },
		"non-blocking contradiction": func(f *IntentReviewFinding) { f.Blocking = false },
		"non-blocking conflict":      func(f *IntentReviewFinding) { f.Kind = IntentReviewConflict; f.Blocking = false },
		"no spans":                   func(f *IntentReviewFinding) { f.Spans = nil },
		"span not verbatim":          func(f *IntentReviewFinding) { f.Spans = []string{"stores notes remotely"} },
		"blank span":                 func(f *IntentReviewFinding) { f.Spans = []string{"  "} },
		"no question":                func(f *IntentReviewFinding) { f.Question = "" },
		"eleven options":             func(f *IntentReviewFinding) { f.Options = strings.Split("a b c d e f g h i j k", " ") },
		"blank option":               func(f *IntentReviewFinding) { f.Options = []string{"a", ""} },
		"duplicate option":           func(f *IntentReviewFinding) { f.Options = []string{"a", "a"} },
		"no recommendation":          func(f *IntentReviewFinding) { f.Recommended = "" },
		"rank 0":                     func(f *IntentReviewFinding) { f.Rank = 0 },
		"long rationale":             func(f *IntentReviewFinding) { f.Rationale = strings.Repeat("r", MaxIntentReviewText+1) },
	} {
		f := sampleFinding()
		bad(&f)
		if f.Validate(reviewContent) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Ten options and non-blocking gaps and ambiguities are fine.
	f := sampleFinding()
	f.Kind, f.Blocking, f.Options = IntentReviewAmbiguity, false, strings.Split("a b c d e f g h i j", " ")
	if err := f.Validate(reviewContent); err != nil {
		t.Fatal(err)
	}
	// Unknown kinds are refused on decode-then-validate.
	var g IntentReviewFinding
	_ = json.Unmarshal([]byte(`{"kind":"omission","spans":["Users are teams."],"question":"q","recommended":"r","rank":1}`), &g)
	if g.Validate(reviewContent) == nil {
		t.Fatal("kind omission is not an intent review kind")
	}
}
