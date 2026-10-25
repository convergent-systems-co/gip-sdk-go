package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// strict decodes s into out, refusing unknown fields, as gip's schema
// tests decode the SDK's types.
func strict(t *testing.T, s string, out any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("strict decode of %s: %v", s, err)
	}
}

// encodes fails unless v encodes to JSON containing every want and none of
// the absent strings.
func encodes(t *testing.T, v any, want []string, absent ...string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(b), w) {
			t.Errorf("%s lacks %s", b, w)
		}
	}
	for _, a := range absent {
		if strings.Contains(string(b), a) {
			t.Errorf("%s carries %s", b, a)
		}
	}
}

// TestDarkFactoryPlanDocument pins the v0.9.0 plan document additions:
// autonomous, the recalled intents, open questions, human_due, the
// delivery record, finding kinds and timing, work timing, re-scopes and
// the new triggers. A document without them encodes as before.
func TestDarkFactoryPlanDocument(t *testing.T) {
	doc := `{"schema":"gip.plan-document/v1","algorithm":"gip.plan/1","project_id":"p","state_head":40,"pass":5,"planned":true,"root":"A",
	"evaluated_at":"2026-10-19T12:00:00Z","ready":[],"in_flight":[],"counts":{},"milestones":[],
	"triggers":[{"kind":"rescope","scope":"B","reason":"r","findings":["finding-3"]},{"kind":"refine","scope":"A","reason":"q","objects":["amb-2"]}],
	"findings":[{"id":"finding-3","node":"B1","title":"t","blocking":true,"kind":"rescope","status":"open","recorded_seq":31,"recorded_pass":4},
	            {"id":"finding-1","node":"B2","title":"d","blocking":false,"decision":true,"status":"decided"}],
	"unplanned":[],
	"nodes":[{"address":"B1","parent":"B","branch":"B","depth":2,"label":"x","kind":"wu","ref":"w","pass":1,"lifecycle":"live","status":"complete","reasons":[],
	          "depends_on":[],"children":[],"findings":["finding-3"],"rescopes":1,
	          "work":{"execution":"integrated","frontier_status":"DONE","reasons":[],"completed_seq":29,"completed_pass":4}}],
	"delivery":{"pass":5,"findings":[],"record":{"id":"delivery-1","intents":[{"id":"main","revision":3}],"final_pass":5,"design":{"revision":2,"digest":"sha256:d"},
	  "commit":"abc123","goals":[{"goal":"g1","qualification":"cer-q","acceptance":"cer-a"}],"checks":["ev-1"],
	  "held_questions":[{"id":"amb-4","default":"metric units"}],"basis":"sha256:b","status":"prepared",
	  "prepared":{"principal":"agent:reviewer","request_id":"r","event_seq":39,"at":"2026-10-19T11:59:00Z","source":"plugin:gip-plugin-executor@1.0.0"}}},
	"autonomous":true,
	"intents":[{"id":"main","revision":3,"digest":"sha256:i","content":"# Weather"}],
	"questions":[{"id":"amb-4","intent":"main","for_revision":3,"text":"Which units?","class":"intent","options":["metric","imperial"],"recommended":"metric",
	              "default":"metric units","node":"B","refs":["finding-2"],"status":"open"}],
	"human_due":{"due":true,"reason":"quiescent","questions":["amb-4"],"proposal":"prop-1"},
	"digest":"sha256:x"}`
	var d PlanDocument
	strict(t, doc, &d)
	if d.Triggers[0].Kind != TriggerRescope || d.Triggers[1].Kind != TriggerRefine {
		t.Fatalf("triggers %+v", d.Triggers)
	}
	if f := d.Findings[0]; f.EffectiveKind() != FindingKindRescope || f.RecordedSeq != 31 || f.RecordedPass != 4 {
		t.Fatalf("finding %+v", f)
	}
	if f := d.Findings[1]; f.EffectiveKind() != FindingKindDecision {
		t.Fatalf("the decision flag reads as kind decision: %+v", f)
	}
	if (&PlanFinding{}).EffectiveKind() != FindingKindWork {
		t.Fatal("a finding without a kind is work")
	}
	n, _ := d.Node("B1")
	if n.Rescopes != 1 || n.Work.CompletedSeq != 29 || n.Work.CompletedPass != 4 {
		t.Fatalf("node %+v work %+v", n, n.Work)
	}
	r := d.Delivery.Record
	if r == nil || r.ID != "delivery-1" || !strings.HasPrefix(r.ID, DeliveryRecordPrefix) || r.Status != DeliveryPrepared || r.Intents[0].Revision != 3 ||
		r.Design.Digest != "sha256:d" || r.Goals[0].Acceptance != "cer-a" || r.HeldQuestions[0].Default != "metric units" || r.Prepared.Principal != "agent:reviewer" || r.Accepted != nil {
		t.Fatalf("delivery record %+v", r)
	}
	if d.Autonomous == nil || !*d.Autonomous || d.Intents[0].Content != "# Weather" {
		t.Fatalf("autonomous %v intents %+v", d.Autonomous, d.Intents)
	}
	if q := d.Questions[0]; q.Class != QuestionClassIntent || q.Status != QuestionOpen || q.Blocking || q.Options[1] != "imperial" || q.ForRevision != 3 {
		t.Fatalf("question %+v", q)
	}
	if h := d.HumanDue; h == nil || !h.Due || h.Proposal != "prop-1" || h.Questions[0] != "amb-4" {
		t.Fatalf("human due %+v", h)
	}

	// autonomous false is explicit, not omitted.
	no := false
	encodes(t, PlanDocument{Autonomous: &no}, []string{`"autonomous":false`})
	// Older documents encode as before.
	encodes(t, PlanDocument{}, nil, "autonomous", "intents", "questions", "human_due", "delivery")
	encodes(t, PlanDelivery{Pass: 1, Findings: []string{}}, []string{`"findings":[]`}, "record")
	encodes(t, PlanFinding{ID: "f"}, nil, "kind", "recorded_")
	encodes(t, PlanWork{Reasons: []Reason{}}, nil, "completed_")
	encodes(t, PlanNode{}, nil, "rescopes")
}

// TestPlanPassQuestions pins a pass's questions and the scope dismissal
// that names one, and the result's omitted refs and question ids.
func TestPlanPassQuestions(t *testing.T) {
	p := HostPlanPassParams{As: "agent:planner", Pass: PlanPass{Trigger: TriggerMilestone, Scope: "B", Strategy: "s", ExpectedPass: 4,
		Questions: []PlanQuestion{{Ref: "q1", Text: "Add a pollen goal?", Class: QuestionClassGoal, Options: []string{"yes", "no"}, Recommended: "no", Default: "no", Node: "B", Refs: []string{"finding-2"}}},
		Findings:  []FindingResolution{{Finding: "finding-2", Dismiss: "outside the intent", Grounds: GroundsScope, Question: "q1"}}}}
	encodes(t, p, []string{`"questions":[{"ref":"q1","text":"Add a pollen goal?","class":"goal","options":["yes","no"],"recommended":"no","default":"no","node":"B","refs":["finding-2"]}]`,
		`"grounds":"scope"`, `"question":"q1"`}, "blocking", `"id"`, "status", "for_revision")
	var back HostPlanPassParams
	b, _ := json.Marshal(p)
	strict(t, string(b), &back)
	if back.Pass.Questions[0].Ref != "q1" || back.Pass.Findings[0].Question != "q1" {
		t.Fatalf("round trip %+v", back.Pass)
	}
	encodes(t, PlanPass{Trigger: TriggerInitial, Scope: "A", Strategy: "s"}, nil, "questions")
	encodes(t, FindingResolution{Finding: "f", Dismiss: "stale"}, nil, "grounds", "question")

	var res HostPlanPassResult
	strict(t, `{"disposition":"ACCEPTED","reasons":[],"event_seq":12,"pass":5,"added":["B3"],"refs":{"n":"B3"},"omitted":["g9","n9"],"questions":{"q1":"amb-5","g9":"amb-6"}}`, &res)
	if res.Omitted[1] != "n9" || res.Questions["g9"] != "amb-6" {
		t.Fatalf("result %+v", res)
	}
	encodes(t, HostPlanPassResult{Disposition: "ACCEPTED", Reasons: []string{}}, nil, "omitted", "questions")
}

// TestFindingKinds pins the finding's kind on the wire and the question a
// refused finding becomes.
func TestFindingKinds(t *testing.T) {
	encodes(t, HostFindingParams{Work: "w", As: "agent:reviewer", Title: "rejected three times", Kind: FindingKindRescope}, []string{`"kind":"rescope"`}, "decision", "blocking")
	encodes(t, HostFindingParams{Work: "w", As: "agent:x", Title: "t"}, nil, "kind", "provides")
	encodes(t, HostFindingParams{Work: "w", As: "agent:x", Title: "t", Blocking: true, Kind: FindingKindPrerequisite, Provides: "passive lookup protocol"}, []string{`"kind":"prerequisite"`, `"provides":"passive lookup protocol"`})
	if (&PlanFinding{Kind: FindingKindPrerequisite}).EffectiveKind() != FindingKindPrerequisite {
		t.Fatal("prerequisite kind")
	}
	var r HostFindingResult
	strict(t, `{"disposition":"ACCEPTED","reasons":[],"question":"amb-7","event_seq":3}`, &r)
	if r.Question != "amb-7" {
		t.Fatalf("result %+v", r)
	}
	encodes(t, HostFindingResult{Reasons: []string{}}, nil, "question")
	for _, k := range []string{FindingKindWork, FindingKindDecision, FindingKindRescope, FindingKindSplit, FindingKindPrerequisite} {
		if k == "" {
			t.Fatal("finding kinds are named")
		}
	}
}

// TestIntentProposeAndDelivery pins the two new host methods' messages.
func TestIntentProposeAndDelivery(t *testing.T) {
	if MethodHostIntentPropose != "host/intent/propose" || MethodHostDeliveryPrepare != "host/delivery/prepare" {
		t.Fatal("method names are part of the protocol")
	}
	p := HostIntentProposeParams{As: "agent:planner", Proposal: IntentProposal{Intent: "main", BaseRevision: 3, Content: "# Weather\nmetric", Questions: []string{"amb-4"}}}
	encodes(t, p, []string{`"proposal":{"intent":"main","base_revision":3,"content":"# Weather\nmetric","questions":["amb-4"]}`}, "request_id", "rationale")
	var pr HostIntentProposeResult
	strict(t, `{"disposition":"ACCEPTED","reasons":[],"proposal":"prop-2","event_seq":41}`, &pr)
	if pr.Proposal != "prop-2" {
		t.Fatalf("propose result %+v", pr)
	}

	encodes(t, HostDeliveryPrepareParams{As: "agent:reviewer"}, []string{`{"as":"agent:reviewer"}`})
	var dr HostDeliveryPrepareResult
	strict(t, `{"disposition":"REJECTED","code":"QUESTION_OPEN","reasons":["amb-4 blocks"],"event_seq":0}`, &dr)
	if dr.Code != "QUESTION_OPEN" || dr.Record != nil {
		t.Fatalf("prepare result %+v", dr)
	}
	if CodeRuntimeNotStarted != "RUNTIME_NOT_STARTED" {
		t.Fatal("result codes are part of the protocol")
	}
	var nr HostDeliveryPrepareResult
	strict(t, `{"disposition":"BLOCKED","code":"RUNTIME_NOT_STARTED","reasons":["runtime exited before ready"],"event_seq":0}`, &nr)
	if nr.Disposition != "BLOCKED" || nr.Code != CodeRuntimeNotStarted || nr.Delivery != "" || nr.Record != nil {
		t.Fatalf("runtime-not-started result %+v", nr)
	}
	encodes(t, DeliveryRecord{ID: "delivery-1", Status: DeliveryAccepted}, []string{`"status":"accepted"`}, "prepared", "accepted\":{", "design")
}

// TestGoalActsAndChecks pins the reviewer's goal acts and host/check on a
// goal subject.
func TestGoalActsAndChecks(t *testing.T) {
	if ActGoalQualify != "goal.qualify" || ActGoalAccept != "goal.accept" || ReasonAcceptanceDue != "ACCEPTANCE_DUE" {
		t.Fatal("goal act kinds and the acceptance reason are part of the protocol")
	}
	encodes(t, HostActParams{Kind: ActGoalAccept, Goal: "g1", As: "agent:reviewer", Rationale: "outcome observed", Evidence: []string{"ev-9"}},
		[]string{`"kind":"goal.accept"`, `"goal":"g1"`, `"evidence":["ev-9"]`}, `"ceremony"`, `"outcome"`, `"work"`)
	encodes(t, HostActParams{Kind: ActStart, Work: "w", As: "agent:x"}, nil, "goal")

	if GoalSubject("g1") != "goal:g1" || WorkSubject("w") != "work:w" {
		t.Fatal("subjects are kind:id")
	}
	encodes(t, HostCheckParams{Subject: GoalSubject("g1"), Predicate: "weather.shown", As: "agent:reviewer"}, []string{`"subject":"goal:g1"`})
	encodes(t, HostCheckParams{Work: "w", Predicate: "p", As: "agent:x"}, []string{`"work":"w"`}, "subject")
	var c HostCheckParams
	strict(t, `{"subject":"goal:g1","predicate":"p","as":"agent:reviewer","workspace":"/w"}`, &c)
	if c.Subject != "goal:g1" || !c.IsGoalSubject() || c.ValidateTarget() != nil {
		t.Fatalf("check %+v", c)
	}
	encodes(t, HostCheckParams{Subject: "goal:g1", Predicate: "p", As: "a"}, nil, `"work"`)

	for name, p := range map[string]HostActParams{
		"goal act with work":   {Kind: ActGoalQualify, Goal: "g", Work: "w"},
		"goal act, no goal":    {Kind: ActGoalAccept},
		"work act with a goal": {Kind: ActStart, Work: "w", Goal: "g"},
		"work act, no work":    {Kind: ActSubmit},
	} {
		if p.ValidateTarget() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	for _, p := range []HostActParams{{Kind: ActGoalAccept, Goal: "g"}, {Kind: ActReview, Work: "w"}} {
		if err := p.ValidateTarget(); err != nil {
			t.Errorf("%s: %v", p.Kind, err)
		}
	}
	for name, p := range map[string]HostCheckParams{
		"both":        {Work: "w", Subject: "goal:g"},
		"neither":     {},
		"bad subject": {Subject: "invariant:i"},
		"no id":       {Subject: "goal:"},
	} {
		if p.ValidateTarget() == nil {
			t.Errorf("check %s: want an error", name)
		}
	}
	if err := (&HostCheckParams{Subject: WorkSubject("w")}).ValidateTarget(); err != nil {
		t.Fatal(err)
	}
}

// TestDeliverPermission pins work.deliver: a known permission of its own,
// declared by the plugin before a command may use it.
func TestDeliverPermission(t *testing.T) {
	if PermWorkDeliver != "work.deliver" {
		t.Fatal("work.deliver is part of the protocol")
	}
	found := false
	for _, p := range KnownPermissions {
		found = found || p == PermWorkDeliver
	}
	if !found {
		t.Fatal("work.deliver is a known permission")
	}
	m := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-reviewer","version":"0.1.0","protocol_versions":[1],"executable":"x",
	"permissions":["work.read","work.review","work.deliver"],
	"commands":[{"name":"deliver","summary":"s","permissions":["work.read","work.deliver"]}]}`
	if _, err := ParseManifest([]byte(m)); err != nil {
		t.Fatalf("a reviewer declares work.deliver: %v", err)
	}
	if _, err := ParseManifest([]byte(strings.Replace(m, `"work.review","work.deliver"],`, `"work.review"],`, 1))); err == nil {
		t.Fatal("a command cannot use work.deliver the plugin does not declare")
	}
}

// TestQuestionClosedBy pins closed_by on questions and ambiguities.
func TestQuestionClosedBy(t *testing.T) {
	var q PlanQuestion
	strict(t, `{"id":"amb-1","text":"t","class":"intent","status":"answered","closed_by":{"kind":"revision","ref":"main@4"}}`, &q)
	if q.ClosedBy == nil || q.ClosedBy.Kind != ClosedByRevision || q.ClosedBy.Ref != "main@4" {
		t.Fatalf("question %+v", q)
	}
	var a Ambiguity
	strict(t, `{"id":"amb-2","for_revision":3,"text":"t","consequential":false,"status":"defaulted","closed_by":{"kind":"delivery","ref":"delivery-1"},
	"raised":{"principal":"agent:p","request_id":"r","event_seq":1,"at":"2026-10-19T10:00:00Z"}}`, &a)
	if a.ClosedBy == nil || a.ClosedBy.Kind != ClosedByDelivery || a.Status != QuestionDefaulted {
		t.Fatalf("ambiguity %+v", a)
	}
	encodes(t, PlanQuestion{Text: "t", Class: QuestionClassIntent}, nil, "closed_by")
}

// TestContractReview pins the work contract's review block.
func TestContractReview(t *testing.T) {
	r := HostContractResult{Contract: json.RawMessage(`{"schema":"gip.work-contract/v1","review":{"rejections":3,"rejection_cap":3,"rescopes":0}}`)}
	v, err := r.Review()
	if err != nil || v == nil || v.Rejections != 3 || v.RejectionCap != 3 || !v.AtCap() {
		t.Fatalf("review %+v (%v)", v, err)
	}
	v.Rejections = 2
	if v.AtCap() {
		t.Fatal("below the cap")
	}
	var none *ContractReview
	if none.AtCap() || (&ContractReview{Rejections: 5}).AtCap() {
		t.Fatal("no review, or no cap, is never at the cap")
	}
	for _, c := range []string{`{"schema":"gip.work-contract/v1"}`, ``} {
		if x, err := (&HostContractResult{Contract: json.RawMessage(c)}).Review(); err != nil || x != nil {
			t.Fatalf("a contract without a review block has none: %+v (%v)", x, err)
		}
	}
	if _, err := (&HostContractResult{Contract: json.RawMessage(`{`)}).Review(); err == nil {
		t.Fatal("a malformed contract is an error")
	}
	var back ContractReview
	strict(t, `{"rejections":0,"rejection_cap":3,"rescopes":1}`, &back)
	if back.LastUnreviewable != "" {
		t.Fatalf("no last_unreviewable: %+v", back)
	}
}

// TestContractReviewLastUnreviewable (SDK v0.9.2, gip ADR-0024 §8): the
// review block names the reason when the latest decided review was an
// unreviewable rejection, so the builder resubmits rather than reworks.
func TestContractReviewLastUnreviewable(t *testing.T) {
	r := HostContractResult{Contract: json.RawMessage(`{"schema":"gip.work-contract/v1","review":{"rejections":0,"rejection_cap":3,"rescopes":0,"last_unreviewable":"workspace-changed"}}`)}
	v, err := r.Review()
	if err != nil || v == nil || v.LastUnreviewable != UnreviewableWorkspaceChanged || v.Rejections != 0 || v.AtCap() {
		t.Fatalf("review %+v (%v)", v, err)
	}
	if !IsUnreviewableReason(v.LastUnreviewable) {
		t.Fatalf("last_unreviewable %q is one of UnreviewableReasons", v.LastUnreviewable)
	}
	var back ContractReview
	strict(t, `{"rejections":1,"rejection_cap":3,"rescopes":0,"last_unreviewable":"commit-unknown"}`, &back)
	if back.LastUnreviewable != UnreviewableCommitUnknown {
		t.Fatalf("round trip %+v", back)
	}
	encodes(t, ContractReview{RejectionCap: 3}, nil, "last_unreviewable")
}

// TestProjectSettings pins the settings payload and its defaults.
func TestProjectSettings(t *testing.T) {
	encodes(t, ProjectSettings{}, []string{`{}`})
	two, f, cap5 := RulesDarkFactory, false, 5
	encodes(t, ProjectSettings{Rules: &two, Autonomous: &f, ReviewRejectionCap: &cap5}, []string{`{"rules":2,"autonomous":false,"review_rejection_cap":5}`})
	if v := (ProjectSettings{}).Effective(); v != (ProjectSettingsView{Rules: RulesClassic, Autonomous: true, ReviewRejectionCap: DefaultReviewRejectionCap}) {
		t.Fatalf("defaults %+v", v)
	}
	if v := (ProjectSettings{Rules: &two, Autonomous: &f, ReviewRejectionCap: &cap5}).Effective(); v.Rules != 2 || v.Autonomous || v.ReviewRejectionCap != 5 {
		t.Fatalf("set %+v", v)
	}
	var s ProjectSettings
	strict(t, `{"autonomous":true}`, &s)
	if s.Rules != nil || s.Autonomous == nil || !*s.Autonomous {
		t.Fatalf("decoded %+v", s)
	}
}

// TestResolutionAndAmbiguity pins an agent's resolution basis and span,
// and the question shape of an ambiguity as host/state lists it.
func TestResolutionAndAmbiguity(t *testing.T) {
	encodes(t, IntentResolution{IntentID: "main", AmbiguityID: "amb-1", Resolution: "r"}, nil, "basis", "span")
	encodes(t, IntentResolution{IntentID: "main", AmbiguityID: "amb-1", Resolution: "r", Basis: ResolutionBasisIntent, Span: "s"}, []string{`"basis":"intent"`, `"span":"s"`})

	content := "# Weather\n\nShow the  current temperature\nfor the user's city in metric units."
	if err := CheckResolutionSpan("show the current temperature", content); err == nil {
		t.Fatal("the span is matched verbatim, case included")
	}
	if err := CheckResolutionSpan("Show the current   temperature\n for the user's city", content); err != nil {
		t.Fatalf("whitespace is normalised: %v", err)
	}
	if err := CheckResolutionSpan("metric units.", content); err == nil {
		t.Fatal("a span under 24 bytes or 4 words is refused")
	}
	if err := CheckResolutionSpan("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", strings.Repeat("a", 40)); err == nil {
		t.Fatal("one long word is not 4 words")
	}
	// Lengths are bytes, not characters: four 3-character words of
	// two-byte runes are 27 bytes (15 characters) and pass; the same in
	// ASCII is 15 bytes and fails.
	multi := "Le résumé ééé ééé ééé ééé est ici."
	if err := CheckResolutionSpan("ééé ééé ééé ééé", multi); err != nil {
		t.Fatalf("a 27-byte, 15-character span: %v", err)
	}
	if err := CheckResolutionSpan("eee eee eee eee", "x eee eee eee eee y"); err == nil {
		t.Fatal("a 15-byte span is too short")
	}
	if err := CheckResolutionSpan("ééé ééé ééé \xff\xff\xff\xff\xff\xff\xff\xff", multi); err == nil {
		t.Fatal("invalid UTF-8 is refused")
	}

	var s HostStateResult
	strict(t, `{"project_id":"p","state_head":9,"intent":{"id":"main","revision":2,"interpretation_status":"pending","open_ambiguities":[
	  {"id":"amb-1","for_revision":2,"text":"t","consequential":true,"status":"open","raised":{"principal":"agent:planner","request_id":"r","event_seq":3,"at":"2026-10-19T10:00:00Z"}},
	  {"id":"amb-2","for_revision":2,"text":"u","consequential":false,"class":"decision","options":["a","b"],"recommended":"a","default":"a","node":"B","blocking":true,"refs":["B1"],
	   "status":"resolved","resolution":"x","resolution_basis":"intent","resolution_span":"the four word span",
	   "raised":{"principal":"agent:planner","request_id":"r","event_seq":4,"at":"2026-10-19T10:00:00Z"},
	   "resolved":{"principal":"human:o","request_id":"r2","event_seq":5,"at":"2026-10-19T10:01:00Z","source":"cli","attestation":{"attested_by":"interactive","shown":"sha256:s","basis":"sha256:b"}}},
	  {"id":"amb-3","for_revision":2,"text":"v","consequential":false,"status":"open","raised":{"principal":"agent:planner","request_id":"r","event_seq":6,"at":"2026-10-19T10:00:00Z"}}]},
	"goals":[{"id":"g1","title":"t","outcome":"o","status":"admitted","parent":"","created_seq":2,"requires":["p"],
	  "predicates":[{"predicate":"p","defined":true,"revision":1,"method":"check","check":{"argv":["make","test"]},"code_bound":true,"how_to_satisfy":"run it"}]}],
	"work":[{"id":"w1","goal":"g1","objective":"o","status":"admitted","depends_on":[],"created_seq":3,"review":true,"completed_seq":20,"completed_pass":2}],
	"invariants":[{"id":"inv-1","revision":1,"scope":"project","predicate":"p","mode":"enforce"}]}`, &s)
	a := s.Intent.OpenAmbiguities
	if !a[0].Blocks() || !a[0].IsQuestion() {
		t.Fatal("an old consequential ambiguity is a blocking question")
	}
	if !a[1].IsQuestion() || a[1].ResolutionBasis != ResolutionBasisIntent || a[1].Resolved.Attestation.AttestedBy != "interactive" {
		t.Fatalf("question %+v", a[1])
	}
	if a[2].IsQuestion() {
		t.Fatal("an ambiguity with no class that does not block is not a question")
	}
	if w := s.Work[0]; w.CompletedSeq != 20 || w.CompletedPass != 2 {
		t.Fatalf("work timing %+v", w)
	}
	if string(s.Goals[0].Predicates[0].Check) != `{"argv":["make","test"]}` {
		t.Fatalf("check %s", s.Goals[0].Predicates[0].Check)
	}
	encodes(t, StateWork{ID: "w", DependsOn: []string{}}, nil, "completed_")
	encodes(t, Ambiguity{ID: "a"}, []string{`"consequential":false`}, "class", "options", "blocking", "resolution_")
}

// TestRuntimeBlock pins the design's runtime block: parsing exactly one
// fenced gip-runtime block, strict JSON, and gip's validation rules.
func TestRuntimeBlock(t *testing.T) {
	design := "# Design\n\n```json\n{\"not\":\"it\"}\n```\n\n## Runtime\n\n```gip-runtime\n" +
		`{"start":["go","run","./cmd/server"],"ready":{"url":"http://127.0.0.1:8080/healthz"},"timeout_seconds":60,"env":{"PORT":"8080"}}` + "\n```\n"
	b, err := ParseRuntimeBlock(design)
	if err != nil {
		t.Fatal(err)
	}
	if b.Start[2] != "./cmd/server" || b.Ready.URL == "" || b.TimeoutSeconds != 60 || b.Env["PORT"] != "8080" || b.Validate() != nil {
		t.Fatalf("block %+v (%v)", b, b.Validate())
	}
	fence, err := b.Fence()
	if err != nil || !strings.HasPrefix(fence, "```gip-runtime\n") {
		t.Fatalf("fence %q %v", fence, err)
	}
	again, err := ParseRuntimeBlock("intro\n" + fence)
	if err != nil || again.TimeoutSeconds != 60 {
		t.Fatalf("a rendered block parses back: %+v %v", again, err)
	}

	// A closing fence may be indented up to three spaces and be longer.
	if _, err := ParseRuntimeBlock("```gip-runtime\n{\"none\":true}\n   `````\n"); err != nil {
		t.Fatalf("a closing fence indented three spaces: %v", err)
	}
	none, err := ParseRuntimeBlock("~~~~ gip-runtime\n{\"none\":true}\n~~~~\n")
	if err != nil || !none.None || none.Validate() != nil {
		t.Fatalf("none %+v %v", none, err)
	}
	for name, d := range map[string]string{
		"none":            "# Design\n```go\nx\n```\n",
		"two":             "```gip-runtime\n{\"none\":true}\n```\n```gip-runtime\n{\"none\":true}\n```\n",
		"unknown":         "```gip-runtime\n{\"none\":true,\"shell\":\"sh\"}\n```\n",
		"trailing":        "```gip-runtime\n{\"none\":true} {}\n```\n",
		"inside":          "````md\n```gip-runtime\n{\"none\":true}\n```\n````\n",
		"unclosed":        "# D\n```gip-runtime\n{\"none\":true}\n",
		"unclosed other":  "```gip-runtime\n{\"none\":true}\n```\n```go\nx\n",
		"fence mismatch":  "```gip-runtime\n{\"none\":true}\n~~~\n",
		"short close":     "````gip-runtime\n{\"none\":true}\n```\n",
		"indented close":  "```gip-runtime\n{\"none\":true}\n    ```\n```\n",
		"tab close":       "```gip-runtime\n{\"none\":true}\n\t```\n",
		"duplicate key":   "```gip-runtime\n{\"none\":false,\"none\":true}\n```\n",
		"dup nested":      "```gip-runtime\n{\"start\":[\"x\"],\"ready\":{\"url\":\"http://localhost/\",\"url\":\"http://localhost:2/\"},\"timeout_seconds\":1}\n```\n",
		"dup env":         "```gip-runtime\n{\"start\":[\"x\"],\"ready\":{\"check\":[\"c\"]},\"timeout_seconds\":1,\"env\":{\"A\":\"1\",\"A\":\"2\"}}\n```\n",
		"case key":        "```gip-runtime\n{\"None\":true}\n```\n",
		"case nested key": "```gip-runtime\n{\"start\":[\"x\"],\"ready\":{\"URL\":\"http://localhost/\"},\"timeout_seconds\":1}\n```\n",
		"not an object":   "```gip-runtime\n[1]\n```\n",
		"wrong type":      "```gip-runtime\n{\"timeout_seconds\":\"5\"}\n```\n",
	} {
		if _, err := ParseRuntimeBlock(d); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	for name, bad := range map[string]RuntimeBlock{
		"none and start": {None: true, Start: []string{"x"}},
		"no start":       {Ready: &RuntimeReady{URL: "u"}, TimeoutSeconds: 1},
		"no ready":       {Start: []string{"x"}, TimeoutSeconds: 1},
		"both ready":     {Start: []string{"x"}, Ready: &RuntimeReady{URL: "u", Check: []string{"c"}}, TimeoutSeconds: 1},
		"timeout 0":      {Start: []string{"x"}, Ready: &RuntimeReady{URL: "u"}},
		"timeout 601":    {Start: []string{"x"}, Ready: &RuntimeReady{URL: "u"}, TimeoutSeconds: 601},
		"GIP_ env":       {Start: []string{"x"}, Ready: &RuntimeReady{URL: "u"}, TimeoutSeconds: 1, Env: map[string]string{"GIP_KEY_PASSPHRASE": "x"}},
		"DYLD_ env":      {Start: []string{"x"}, Ready: &RuntimeReady{Check: []string{"c"}}, TimeoutSeconds: 1, Env: map[string]string{"DYLD_INSERT_LIBRARIES": "x"}},
		"LD_ env":        {Start: []string{"x"}, Ready: &RuntimeReady{Check: []string{"c"}}, TimeoutSeconds: 600, Env: map[string]string{"LD_PRELOAD": "x"}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	ok := RuntimeBlock{Start: []string{"x"}, Ready: &RuntimeReady{Check: []string{"curl", "-f", "u"}}, TimeoutSeconds: 600}
	url := func(u string) RuntimeBlock {
		return RuntimeBlock{Start: []string{"x"}, Ready: &RuntimeReady{URL: u}, TimeoutSeconds: 5}
	}
	env := func(k string) RuntimeBlock {
		return RuntimeBlock{Start: []string{"x"}, Ready: &RuntimeReady{URL: "http://127.0.0.1:1/"}, TimeoutSeconds: 5, Env: map[string]string{k: "v"}}
	}
	for name, bad := range map[string]RuntimeBlock{
		"lower gip_":     env("gip_key_passphrase"),
		"mixed Dyld_":    env("Dyld_Insert_Libraries"),
		"lower ld_":      env("ld_preload"),
		"empty key":      env(""),
		"key with =":     env("A=B"),
		"key with NUL":   env("A\x00B"),
		"leading digit":  env("1A"),
		"dash":           env("MY-VAR"),
		"empty check":    {Start: []string{"x"}, Ready: &RuntimeReady{Check: []string{"curl", ""}}, TimeoutSeconds: 5},
		"empty start":    {Start: []string{"x", ""}, Ready: &RuntimeReady{Check: []string{"c"}}, TimeoutSeconds: 5},
		"ftp url":        url("ftp://127.0.0.1/"),
		"remote url":     url("http://example.com/healthz"),
		"lookalike host": url("http://127.0.0.1.example.com/"),
		"userinfo":       url("http://u:p@localhost/"),
		"no scheme":      url("localhost:8080"),
	} {
		if bad.Validate() == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	for _, u := range []string{"http://127.0.0.1:8080/healthz", "https://localhost/", "http://[::1]:9000/"} {
		if b := url(u); b.Validate() != nil {
			t.Errorf("%s: %v", u, b.Validate())
		}
	}
	if b := env("PORT_2"); b.Validate() != nil {
		t.Fatal(b.Validate())
	}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}
