package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Dark factory completion (additive in SDK v0.9.0; gip ADR-0024, batch B).
//
// A person is asked exactly three things: changing or extending the
// intent, adding a goal the intent does not contain, and a decision that
// fits neither and has no clear answer. Each is a question (an intent
// ambiguity with the shape below), answered by a human revision of the
// intent; a planner proposes that revision (host/intent/propose). The
// project's autonomous setting decides when the person is asked
// (HumanDue). A repeatedly rejected unit is re-scoped once before anyone
// is asked (finding kinds, TriggerRescope). Completion is one human
// decision per delivery: a reviewer agent prepares the delivery record
// (host/delivery/prepare), and a person accepts it at a terminal.
//
// Questions, proposals, deliveries and re-scope exist only on rules 2
// projects (RulesDarkFactory); on rules 1 gip refuses them
// (RULES_REQUIRED). A gip that predates a field never sends it, and
// refuses a request that names it.
const (
	// MethodHostIntentPropose records a proposed intent revision as a
	// trusted agent principal (plan.write). It needs no project grant
	// (gip ADR-0003 §4), is attributed, and changes nothing until a
	// person adopts it.
	MethodHostIntentPropose = "host/intent/propose"
	// MethodHostDeliveryPrepare has gip prepare the delivery for the
	// current intent revisions as a trusted reviewer principal
	// (work.review; the delivery.prepare grant, which no wildcard covers).
	MethodHostDeliveryPrepare = "host/delivery/prepare"
)

// Rules epochs (gip ADR-0024 §1). A project record without rules is at 1;
// new projects start at 2. Only a human raises the epoch, and it never
// falls.
const (
	RulesClassic     = 1
	RulesDarkFactory = 2
)

// Finding kinds (gip ADR-0024 D7). Absent means FindingKindWork; the
// older Decision flag reads as FindingKindDecision (PlanFinding.EffectiveKind).
const (
	FindingKindWork     = "work"
	FindingKindDecision = "decision"
	// FindingKindRescope: the unit's reviews were rejected up to the cap
	// (ContractReview.AtCap); re-plan it.
	FindingKindRescope = "rescope"
	// FindingKindSplit: the unit is too large to review; split it. It
	// counts as a re-scope and needs no rejections.
	FindingKindSplit = "split"
)

// Question classes: the three things a person is asked.
const (
	// QuestionClassIntent: changing or extending the intent.
	QuestionClassIntent = "intent"
	// QuestionClassGoal: adding a goal the intent does not contain.
	QuestionClassGoal = "goal"
	// QuestionClassDecision: a decision that fits neither intent nor goal
	// and has no clear answer.
	QuestionClassDecision = "decision"
)

// Question (intent ambiguity) statuses. Only a human's answering intent
// revision (answered, or withdrawn when not answered) or a delivery
// acceptance (defaulted) closes a question; agents never do.
const (
	QuestionOpen      = "open"
	QuestionResolved  = "resolved"
	QuestionAnswered  = "answered"
	QuestionDefaulted = "defaulted"
	QuestionWithdrawn = "withdrawn"
)

// PlanQuestion is a question (gip ADR-0024 §6): an intent ambiguity with
// a class, options, a recommendation, the default it closes with if a
// delivery is accepted while it is held, the plan node it concerns, and
// whether it blocks that node.
//
// In a pass (PlanPass.Questions) the planner sets Ref (to name it from a
// scope dismissal and in the result) and the question's shape; gip
// assigns the id. In the plan document (PlanDocument.Questions) gip sets
// ID, Intent, ForRevision and Status and leaves Ref empty.
//
// A blocking question holds its node (QUESTION_BLOCKING) and the
// admission of interpretations of its revision (QUESTION_OPEN); a
// non-blocking one holds nothing and is closed with its Default when the
// person accepts the delivery.
type PlanQuestion struct {
	Ref         string   `json:"ref,omitempty"`
	ID          string   `json:"id,omitempty"`
	Intent      string   `json:"intent,omitempty"`
	ForRevision int      `json:"for_revision,omitempty"`
	Text        string   `json:"text"`
	Span        string   `json:"span,omitempty"`
	Class       string   `json:"class"`
	Options     []string `json:"options,omitempty"`
	Recommended string   `json:"recommended,omitempty"`
	Default     string   `json:"default,omitempty"`
	Node        string   `json:"node,omitempty"`
	Blocking    bool     `json:"blocking,omitempty"`
	// Refs name findings and plan nodes the question arose from.
	Refs   []string `json:"refs,omitempty"`
	Status string   `json:"status,omitempty"`
}

// PlanIntent is one current intent revision as the plan document carries
// it (gip ADR-0024 D4), content included, so a planner recalls it in
// every planning prompt.
type PlanIntent struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
	Content  string `json:"content"`
}

// HumanDue says whether the person is due to be asked (gip ADR-0024 §6).
// Due: with autonomous false, once an open question is covered by a
// pending proposal or a refine pass proposed nothing for it; with
// autonomous true, only when the project is quiescent; either way when
// a blocking question holds the root or every remaining branch.
// Questions are the question ids to ask, Proposal the pending proposal
// covering them. Executors stop electing while Due, and report it.
type HumanDue struct {
	Due       bool     `json:"due"`
	Reason    string   `json:"reason,omitempty"`
	Questions []string `json:"questions,omitempty"`
	Proposal  string   `json:"proposal,omitempty"`
}

// IntentProposal is a proposed intent revision (gip ADR-0024 §6): Content
// is the full new intent text, written against BaseRevision of intent
// Intent, and Questions are the ids of the open questions it covers. A
// newer proposal from the same principal for the same base supersedes
// the older one. Content is bounded like an intent expression.
type IntentProposal struct {
	Intent       string   `json:"intent"`
	BaseRevision int      `json:"base_revision"`
	Content      string   `json:"content"`
	Questions    []string `json:"questions,omitempty"`
}

// HostIntentProposeParams submits a proposal as principal As.
type HostIntentProposeParams struct {
	As        string         `json:"as"`
	RequestID string         `json:"request_id,omitempty"`
	Rationale string         `json:"rationale,omitempty"`
	Proposal  IntentProposal `json:"proposal"`
}

// HostIntentProposeResult is the gateway's disposition and the recorded
// proposal's id.
type HostIntentProposeResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Proposal    string   `json:"proposal,omitempty"`
	EventSeq    int64    `json:"event_seq"`
}

// Delivery statuses (gip ADR-0024 §7).
const (
	DeliveryPrepared   = "prepared"
	DeliveryAccepted   = "accepted"
	DeliverySuperseded = "superseded"
)

// DeliveryRecordPrefix is the reserved id prefix of delivery records.
const DeliveryRecordPrefix = "delivery-"

// DeliveryRecord is one delivery (gip delivery.v1, ADR-0024 §7): the
// intent revisions it delivers, the final pass and design it was planned
// to, the integration commit, each root and first-level goal's
// qualification and acceptance, the evidence of their live checks
// against the running outcome, the non-blocking questions held open with
// the defaults they close with, and the basis digest delivery.accept
// recomputes (STALE_DELIVERY when it changed). A new intent revision
// supersedes a prepared delivery; an accepted one stays accepted for the
// revisions it covers.
type DeliveryRecord struct {
	ID            string           `json:"id"`
	Intents       []IntentRevision `json:"intents"`
	FinalPass     int              `json:"final_pass"`
	Design        *DesignPin       `json:"design,omitempty"`
	Commit        string           `json:"commit"`
	Goals         []DeliveryGoal   `json:"goals"`
	Checks        []string         `json:"checks"`
	HeldQuestions []HeldQuestion   `json:"held_questions"`
	Basis         string           `json:"basis"`
	Status        string           `json:"status"`
	Prepared      *Provenance      `json:"prepared,omitempty"`
	Accepted      *Provenance      `json:"accepted,omitempty"`
}

// IntentRevision names one revision of an intent.
type IntentRevision struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// DeliveryGoal is a delivered goal with references to the records of its
// qualification and acceptance.
type DeliveryGoal struct {
	Goal          string `json:"goal"`
	Qualification string `json:"qualification,omitempty"`
	Acceptance    string `json:"acceptance,omitempty"`
}

// HeldQuestion is a non-blocking question a delivery holds open, with the
// default it closes with when the person accepts the delivery.
type HeldQuestion struct {
	ID       string `json:"id"`
	Default  string `json:"default,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
}

// Provenance records who made a governed change and when (gip's
// provenance). Attestation is how a human's decision was made.
type Provenance struct {
	Principal   string       `json:"principal"`
	RequestID   string       `json:"request_id"`
	EventSeq    int64        `json:"event_seq"`
	At          time.Time    `json:"at"`
	Source      string       `json:"source,omitempty"`
	Attestation *Attestation `json:"attestation,omitempty"`
}

// Attestation: AttestedBy is "interactive" (or, in records made before
// proxies were retired, "proxy:<name>"); Shown and Basis are the digests
// of the screen the person confirmed and of its state basis.
type Attestation struct {
	AttestedBy string `json:"attested_by"`
	Shown      string `json:"shown,omitempty"`
	Basis      string `json:"basis,omitempty"`
}

// HostDeliveryPrepareParams asks gip to prepare the delivery as principal
// As, the reviewer. When the final discovery ran for the current intent
// revisions and the root is terminal, gip makes a temporary worktree of
// the integration branch tip, starts the design's RuntimeBlock there,
// runs every root and first-level goal's outcome checks against it, has
// the reviewer qualify and accept the goals (conflict of interest
// applies), and records the delivery. It refuses while a blocking
// question is open.
type HostDeliveryPrepareParams struct {
	As        string `json:"as"`
	RequestID string `json:"request_id,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// HostDeliveryPrepareResult is the gateway's disposition and the prepared
// record.
type HostDeliveryPrepareResult struct {
	Disposition string          `json:"disposition"`
	Code        string          `json:"code,omitempty"`
	Reasons     []string        `json:"reasons"`
	Delivery    string          `json:"delivery,omitempty"`
	Record      *DeliveryRecord `json:"record,omitempty"`
	EventSeq    int64           `json:"event_seq"`
}

// Project settings defaults and bounds (gip ADR-0024 §2).
const (
	DefaultReviewRejectionCap = 3
	MaxReviewRejectionCap     = 100
)

// ProjectSettings is the payload of gip's project.settings transition
// (gip project settings): human-only under both epochs, never available
// to a plugin. Nil fields are unchanged. Rules only rises; Autonomous
// absent from the project means true; ReviewRejectionCap (1 to
// MaxReviewRejectionCap) absent means DefaultReviewRejectionCap.
type ProjectSettings struct {
	Rules              *int  `json:"rules,omitempty"`
	Autonomous         *bool `json:"autonomous,omitempty"`
	ReviewRejectionCap *int  `json:"review_rejection_cap,omitempty"`
}

// ProjectSettingsView is a project's effective settings, the defaults
// filled in.
type ProjectSettingsView struct {
	Rules              int  `json:"rules"`
	Autonomous         bool `json:"autonomous"`
	ReviewRejectionCap int  `json:"review_rejection_cap"`
}

// Effective returns the settings a project record with these (possibly
// absent) values has: rules 1, autonomous true and a cap of 3 when
// absent.
func (s ProjectSettings) Effective() ProjectSettingsView {
	v := ProjectSettingsView{Rules: RulesClassic, Autonomous: true, ReviewRejectionCap: DefaultReviewRejectionCap}
	if s.Rules != nil && *s.Rules > 0 {
		v.Rules = *s.Rules
	}
	if s.Autonomous != nil {
		v.Autonomous = *s.Autonomous
	}
	if s.ReviewRejectionCap != nil && *s.ReviewRejectionCap > 0 {
		v.ReviewRejectionCap = *s.ReviewRejectionCap
	}
	return v
}

// Ambiguity resolution by an agent (gip ADR-0024 §4).
const (
	// ResolutionBasisIntent is the only basis on which an agent resolves
	// an ambiguity: the current intent revision's own text settles it.
	ResolutionBasisIntent = "intent"
	// MinResolutionSpanBytes and MinResolutionSpanWords bound the span an
	// agent quotes (after whitespace normalisation).
	MinResolutionSpanBytes = 24
	MinResolutionSpanWords = 4
)

// IntentResolution is the payload of gip's intent.resolve. Under rules 2
// an agent's resolution needs Basis ResolutionBasisIntent and Span, a
// verbatim (whitespace-normalised) part of the current intent revision
// (RESOLUTION_BASIS_REQUIRED otherwise); an agent may resolve only an
// ambiguity that is not a question and that neither it nor its pass
// raised (QUESTION_NOT_RESOLVABLE).
type IntentResolution struct {
	IntentID    string `json:"intent_id"`
	AmbiguityID string `json:"ambiguity_id"`
	Resolution  string `json:"resolution"`
	Basis       string `json:"basis,omitempty"`
	Span        string `json:"span,omitempty"`
}

// NormalizeSpace collapses every run of white space to one space and
// trims the ends, as gip does before matching a resolution span.
func NormalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// CheckResolutionSpan reports why span would not settle an ambiguity
// from intent text content (gip's own check is authoritative): it must
// be valid UTF-8, at least MinResolutionSpanBytes bytes and
// MinResolutionSpanWords words, and occur verbatim in content, both
// whitespace-normalised.
func CheckResolutionSpan(span, content string) error {
	n := NormalizeSpace(span)
	if !utf8.ValidString(n) || len(n) < MinResolutionSpanBytes || len(strings.Fields(n)) < MinResolutionSpanWords {
		return fmt.Errorf("the span must be at least %d bytes and %d words of the current intent revision", MinResolutionSpanBytes, MinResolutionSpanWords)
	}
	if !strings.Contains(NormalizeSpace(content), n) {
		return errors.New("the span does not occur verbatim in the current intent revision")
	}
	return nil
}

// Ambiguity is an intent ambiguity as gip records it (intent.v1), the
// shape host/state lists in intent.open_ambiguities. Class, Options,
// Recommended, Default, Node, Blocking and Refs (SDK v0.9.0) make it a
// question; old records read Consequential as Blocking (IsQuestion,
// Blocks). ResolutionBasis and ResolutionSpan record an agent's
// resolution.
type Ambiguity struct {
	ID              string      `json:"id"`
	ForRevision     int         `json:"for_revision"`
	Text            string      `json:"text"`
	Span            string      `json:"span,omitempty"`
	Consequential   bool        `json:"consequential"`
	Class           string      `json:"class,omitempty"`
	Options         []string    `json:"options,omitempty"`
	Recommended     string      `json:"recommended,omitempty"`
	Default         string      `json:"default,omitempty"`
	Node            string      `json:"node,omitempty"`
	Blocking        bool        `json:"blocking,omitempty"`
	Refs            []string    `json:"refs,omitempty"`
	Status          string      `json:"status"`
	Resolution      string      `json:"resolution,omitempty"`
	ResolutionBasis string      `json:"resolution_basis,omitempty"`
	ResolutionSpan  string      `json:"resolution_span,omitempty"`
	Raised          Provenance  `json:"raised"`
	Resolved        *Provenance `json:"resolved,omitempty"`
}

// Blocks reports whether the ambiguity blocks: Blocking, or, for a record
// made before questions, Consequential.
func (a *Ambiguity) Blocks() bool { return a.Blocking || a.Consequential }

// IsQuestion reports whether the ambiguity is a question: it has a class,
// or it blocks. Agents never resolve questions.
func (a *Ambiguity) IsQuestion() bool { return a.Class != "" || a.Blocks() }

// HostStateResult is the read-only governed state host/state returns
// (state.read): decode into it with Request.State. It mirrors gip's
// StateView.
type HostStateResult struct {
	ProjectID  string           `json:"project_id"`
	StateHead  int64            `json:"state_head"`
	Intent     *StateIntent     `json:"intent,omitempty"`
	Goals      []StateGoal      `json:"goals"`
	Work       []StateWork      `json:"work"`
	Invariants []StateInvariant `json:"invariants"`
}

// StateIntent summarises the intent without its content.
type StateIntent struct {
	ID                   string      `json:"id"`
	Revision             int         `json:"revision"`
	InterpretationStatus string      `json:"interpretation_status"`
	OpenAmbiguities      []Ambiguity `json:"open_ambiguities"`
}

// StateGoal is a goal: its parent and creation seq rebuild the tree,
// Requires and Predicates are its outcome checks.
type StateGoal struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Outcome    string           `json:"outcome"`
	Status     string           `json:"status"`
	Parent     string           `json:"parent,omitempty"`
	CreatedSeq int64            `json:"created_seq"`
	Requires   []string         `json:"requires,omitempty"`
	Predicates []StatePredicate `json:"predicates,omitempty"`
}

// StatePredicate is a predicate's definition as plugins see it. Check is
// the defined check (argv and its settings), passed through as gip sends
// it.
type StatePredicate struct {
	Predicate    string          `json:"predicate"`
	Defined      bool            `json:"defined"`
	Revision     int             `json:"revision,omitempty"`
	Description  string          `json:"description,omitempty"`
	Method       string          `json:"method,omitempty"`
	Check        json.RawMessage `json:"check,omitempty"`
	CodeBound    bool            `json:"code_bound"`
	Paths        []string        `json:"paths,omitempty"`
	HowToSatisfy string          `json:"how_to_satisfy"`
}

// StateWork is a work unit. CompletedSeq and CompletedPass (SDK v0.9.0,
// gip PLAN-036) are set once it is complete, as on PlanWork.
type StateWork struct {
	ID            string   `json:"id"`
	Goal          string   `json:"goal"`
	Objective     string   `json:"objective"`
	Status        string   `json:"status"`
	DependsOn     []string `json:"depends_on"`
	CreatedSeq    int64    `json:"created_seq"`
	Requires      []string `json:"requires,omitempty"`
	Review        bool     `json:"review"`
	CompletedSeq  int64    `json:"completed_seq,omitempty"`
	CompletedPass int      `json:"completed_pass,omitempty"`
}

// StateInvariant is an effective invariant.
type StateInvariant struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	Scope     string `json:"scope"`
	Predicate string `json:"predicate"`
	Mode      string `json:"mode"`
}

// The runtime block (gip ADR-0024 §7, owner answer 4). Under rules 2 a
// design set by an initial or final pass must contain exactly one fenced
// block with the info string RuntimeInfoString (RUNTIME_REQUIRED), whose
// JSON says how to start the outcome so a delivery's live checks run
// against it. It is a planning artifact and grants nothing; gip runs it
// like a check (no shell, its own process group, killed afterwards) in a
// throwaway worktree of the integration branch.
const (
	RuntimeInfoString     = "gip-runtime"
	MinRuntimeTimeoutSecs = 1
	MaxRuntimeTimeoutSecs = 600
)

// RuntimeRefusedEnvPrefixes are environment key prefixes a runtime block
// may not set.
var RuntimeRefusedEnvPrefixes = []string{"GIP_", "DYLD_", "LD_"}

// RuntimeBlock is the runtime block's JSON: Start is the argv that starts
// the outcome, Ready how gip knows it is up, TimeoutSeconds how long it
// waits (1 to 600), Env extra environment. None (alone) says the outcome
// runs no process.
type RuntimeBlock struct {
	None           bool              `json:"none,omitempty"`
	Start          []string          `json:"start,omitempty"`
	Ready          *RuntimeReady     `json:"ready,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// RuntimeReady is exactly one of a URL gip polls until it answers and a
// check argv gip runs until it exits 0.
type RuntimeReady struct {
	URL   string   `json:"url,omitempty"`
	Check []string `json:"check,omitempty"`
}

// Validate checks the block as gip does (gip's check is authoritative).
func (b *RuntimeBlock) Validate() error {
	if b.None {
		if len(b.Start) > 0 || b.Ready != nil || b.TimeoutSeconds != 0 || len(b.Env) > 0 {
			return errors.New("a runtime block with none: true has no other fields")
		}
		return nil
	}
	var errs []error
	if len(b.Start) == 0 || b.Start[0] == "" {
		errs = append(errs, errors.New("start must be a non-empty argv (or none: true)"))
	}
	if b.Ready == nil || (b.Ready.URL == "") == (len(b.Ready.Check) == 0) {
		errs = append(errs, errors.New("ready must name exactly one of url and check"))
	}
	if b.TimeoutSeconds < MinRuntimeTimeoutSecs || b.TimeoutSeconds > MaxRuntimeTimeoutSecs {
		errs = append(errs, fmt.Errorf("timeout_seconds must be %d to %d", MinRuntimeTimeoutSecs, MaxRuntimeTimeoutSecs))
	}
	for k := range b.Env {
		for _, p := range RuntimeRefusedEnvPrefixes {
			if strings.HasPrefix(k, p) {
				errs = append(errs, fmt.Errorf("env key %q may not start with %s", k, p))
			}
		}
	}
	return errors.Join(errs...)
}

// Fence renders the block as the fenced markdown a design carries.
func (b *RuntimeBlock) Fence() (string, error) {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return "```" + RuntimeInfoString + "\n" + string(raw) + "\n```\n", nil
}

// ParseRuntimeBlock finds the design's runtime block and decodes it
// strictly. It is an error when the design holds none or more than one,
// or when the JSON is malformed or has unknown fields; the block is not
// validated (call Validate).
func ParseRuntimeBlock(design string) (*RuntimeBlock, error) {
	var bodies []string
	lines := strings.Split(design, "\n")
	for i := 0; i < len(lines); i++ {
		open := strings.TrimRight(lines[i], " \t\r")
		fence, info, ok := fenceOpen(open)
		if !ok {
			continue
		}
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			if c := strings.TrimSpace(lines[j]); strings.HasPrefix(c, fence) && strings.Trim(c, fence[:1]) == "" && len(c) >= len(fence) {
				break
			}
			body = append(body, lines[j])
		}
		if info == RuntimeInfoString {
			bodies = append(bodies, strings.Join(body, "\n"))
		}
		i = j
	}
	switch len(bodies) {
	case 0:
		return nil, fmt.Errorf("the design has no %s block", RuntimeInfoString)
	case 1:
	default:
		return nil, fmt.Errorf("the design has %d %s blocks; exactly one is allowed", len(bodies), RuntimeInfoString)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(bodies[0])))
	dec.DisallowUnknownFields()
	var b RuntimeBlock
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("%s block: %w", RuntimeInfoString, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s block: more than one JSON value", RuntimeInfoString)
	}
	return &b, nil
}

// fenceOpen reports whether line opens a fenced code block (CommonMark:
// up to three spaces, then three or more backticks or tildes), returning
// the fence and the info string's first word.
func fenceOpen(line string) (fence, info string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return "", "", false
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return "", "", false
	}
	rest := strings.TrimSpace(t[n:])
	if t[0] == '`' && strings.Contains(rest, "`") {
		return "", "", false
	}
	if f := strings.Fields(rest); len(f) > 0 {
		info = f[0]
	}
	return t[:n], info, true
}
