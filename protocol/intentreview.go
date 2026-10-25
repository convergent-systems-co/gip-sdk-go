package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Intent review (SDK v0.12.2). When a person sets an intent revision, gip
// asks a reviewing plugin to inspect the text for contradictions, gaps,
// ambiguities and conflicts (with goals, invariants or the repository) and
// to phrase each as a question with a recommended answer. gip, not the
// plugin, drives the interactive loop: it records the findings as intent
// questions (class intent, refs IntentReviewRef and "kind:<kind>") and asks
// the person one at a time. A plugin never prompts the person.
//
// The plugin declares capability CapReviewIntent and exactly one leaf
// command with role RoleIntentReview. That command takes one required
// string argument, IntentReviewRequestArg, holding the JSON-encoded
// IntentReviewRequest, and returns the IntentReviewResult as its
// InvokeResult.Output. Cost goes in InvokeResult.Cost as for any command.

// CapReviewIntent: the plugin reviews intent text and returns findings
// phrased as questions (SDK v0.12.2). It confers no authority: findings
// become questions only through gip, and only a person answers them.
const CapReviewIntent = "review.intent"

// RoleIntentReview is the command role of an intent reviewer's command
// (SDK v0.12.2).
const RoleIntentReview = "intent.review"

// KnownCommandRoles is the closed set of command roles (Command.Role).
var KnownCommandRoles = []string{RoleIntentReview}

// roleCapability is the capability a plugin must declare to give a
// command each role.
var roleCapability = map[string]string{
	RoleIntentReview: CapReviewIntent,
}

// RoleCapability returns the capability a command role requires, or "".
func RoleCapability(role string) string { return roleCapability[role] }

// IntentReviewRequestArg is the name of the intent.review command's one
// required argument: a string holding the JSON-encoded IntentReviewRequest.
const IntentReviewRequestArg = "request"

// Finding kinds of an intent review (IntentReviewFinding.Kind).
const (
	// IntentReviewContradiction: two parts of the intent cannot both hold.
	// Always blocking.
	IntentReviewContradiction = "contradiction"
	// IntentReviewGap: something the outcome depends on is missing.
	IntentReviewGap = "gap"
	// IntentReviewAmbiguity: the text admits readings that lead to
	// different outcomes.
	IntentReviewAmbiguity = "ambiguity"
	// IntentReviewConflict: the intent conflicts with an existing goal,
	// invariant or the repository. Always blocking.
	IntentReviewConflict = "conflict"
)

// IntentReviewKinds returns the finding kinds in a stable order.
func IntentReviewKinds() []string {
	return []string{IntentReviewContradiction, IntentReviewGap, IntentReviewAmbiguity, IntentReviewConflict}
}

// IsIntentReviewKind reports whether k is an intent review finding kind.
func IsIntentReviewKind(k string) bool { return contains(IntentReviewKinds(), k) }

// IntentReviewKindAlwaysBlocks reports whether every finding of kind k
// must be blocking (contradictions and conflicts).
func IntentReviewKindAlwaysBlocks(k string) bool {
	return k == IntentReviewContradiction || k == IntentReviewConflict
}

// Refs gip puts on a question raised from an intent review finding.
const (
	// IntentReviewRef marks a question as raised by an intent review.
	IntentReviewRef = "intent-review"
	// IntentReviewKindRefPrefix precedes the finding kind ("kind:gap").
	IntentReviewKindRefPrefix = "kind:"
)

// Limits of an intent review. They match gip's intent limits (question
// text, options and option text, questions per request).
const (
	MaxIntentReviewContentBytes  = 256 << 10
	MaxIntentReviewQuestions     = 20
	MaxIntentReviewOptions       = 10
	MaxIntentReviewOptionText    = 500
	MaxIntentReviewText          = 5000
	MaxIntentReviewSpans         = 10
	DefaultIntentReviewQuestions = 5
)

// IntentReviewRequest is what gip sends the reviewer: the intent revision
// in full, the questions already open, deferred or answered (so the
// reviewer does not raise them again), cheap context, and limits.
type IntentReviewRequest struct {
	// Intent and Revision name the revision under review; Content is its
	// full text, including any "## Clarifications" section.
	Intent   string `json:"intent"`
	Revision int    `json:"revision"`
	Content  string `json:"content"`
	// OpenQuestions are the intent's open questions, DeferredQuestions the
	// ones the person deferred, AnsweredQuestions the ones already
	// answered. gip sets ID, Text and Refs (and may set more); the
	// reviewer must not raise any of them again.
	OpenQuestions     []PlanQuestion `json:"open_questions,omitempty"`
	DeferredQuestions []PlanQuestion `json:"deferred_questions,omitempty"`
	AnsweredQuestions []PlanQuestion `json:"answered_questions,omitempty"`
	// Goals, Invariants and RepoSummary are optional context for
	// conflict findings, in host/state shapes.
	Goals       []StateGoal      `json:"goals,omitempty"`
	Invariants  []StateInvariant `json:"invariants,omitempty"`
	RepoSummary string           `json:"repo_summary,omitempty"`
	// MaxQuestions bounds the findings of this round (1 to
	// MaxIntentReviewQuestions); Round counts review rounds on this
	// intent from 1.
	MaxQuestions int `json:"max_questions"`
	Round        int `json:"round"`
}

// Validate checks the request's shape.
func (r *IntentReviewRequest) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf("intent review request: "+format, a...)) }
	if strings.TrimSpace(r.Intent) == "" {
		add("intent must not be empty")
	}
	if r.Revision < 1 {
		add("revision must be at least 1")
	}
	if strings.TrimSpace(r.Content) == "" {
		add("content must not be empty")
	}
	if len(r.Content) > MaxIntentReviewContentBytes || !utf8.ValidString(r.Content) {
		add("content must be valid UTF-8 of at most %d bytes", MaxIntentReviewContentBytes)
	}
	if r.MaxQuestions < 1 || r.MaxQuestions > MaxIntentReviewQuestions {
		add("max_questions must be between 1 and %d", MaxIntentReviewQuestions)
	}
	if r.Round < 1 {
		add("round must be at least 1")
	}
	return errors.Join(errs...)
}

// Arguments encodes the request as the intent.review command's arguments.
func (r *IntentReviewRequest) Arguments() (map[string]any, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return map[string]any{IntentReviewRequestArg: string(b)}, nil
}

// DecodeIntentReviewRequest decodes and validates the request from the
// intent.review command's arguments. Unknown fields are ignored, so a
// newer gip may add context.
func DecodeIntentReviewRequest(args map[string]any) (*IntentReviewRequest, error) {
	s, ok := args[IntentReviewRequestArg].(string)
	if !ok {
		return nil, fmt.Errorf("intent review request: argument %q must be a string", IntentReviewRequestArg)
	}
	var r IntentReviewRequest
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, fmt.Errorf("intent review request: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// IntentReviewFinding is one finding, phrased as a question for the person.
type IntentReviewFinding struct {
	// Kind is one of IntentReviewKinds.
	Kind string `json:"kind"`
	// Spans are verbatim excerpts of the intent text the finding is about
	// (whitespace-normalised match), at least one.
	Spans []string `json:"spans"`
	// Question is what to ask the person.
	Question string `json:"question"`
	// Options are suggested answers, at most MaxIntentReviewOptions.
	Options []string `json:"options,omitempty"`
	// Recommended is the recommended answer. It need not be one of
	// Options. For a non-blocking finding it is also the default.
	Recommended string `json:"recommended"`
	// Blocking: the answer changes outcome, cost, risk or scope.
	// Contradictions and conflicts are always blocking.
	Blocking bool `json:"blocking,omitempty"`
	// Rank orders findings by consequence: 1 is the most consequential.
	// Ranks are unique within a result.
	Rank int `json:"rank"`
	// Rationale says why this matters and why the recommendation.
	Rationale string `json:"rationale,omitempty"`
}

// Validate checks the finding against the intent content under review.
func (f *IntentReviewFinding) Validate(content string) error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if !IsIntentReviewKind(f.Kind) {
		add("kind %q must be one of %v", f.Kind, IntentReviewKinds())
	}
	if IntentReviewKindAlwaysBlocks(f.Kind) && !f.Blocking {
		add("a %s is always blocking", f.Kind)
	}
	if len(f.Spans) == 0 || len(f.Spans) > MaxIntentReviewSpans {
		add("spans must hold 1 to %d excerpts", MaxIntentReviewSpans)
	}
	norm := NormalizeSpace(content)
	for _, s := range f.Spans {
		n := NormalizeSpace(s)
		switch {
		case n == "" || len(s) > MaxIntentReviewText:
			add("a span must be non-empty and at most %d bytes", MaxIntentReviewText)
		case !strings.Contains(norm, n):
			add("span %q does not occur verbatim in the intent revision", s)
		}
	}
	if strings.TrimSpace(f.Question) == "" || len(f.Question) > MaxIntentReviewText {
		add("question must be non-empty and at most %d bytes", MaxIntentReviewText)
	}
	if len(f.Options) > MaxIntentReviewOptions {
		add("at most %d options", MaxIntentReviewOptions)
	}
	for _, o := range f.Options {
		if strings.TrimSpace(o) == "" || len(o) > MaxIntentReviewOptionText {
			add("an option must be non-empty and at most %d bytes", MaxIntentReviewOptionText)
		}
	}
	if dup := firstDuplicate(f.Options); dup != "" {
		add("option %q given twice", dup)
	}
	if strings.TrimSpace(f.Recommended) == "" || len(f.Recommended) > MaxIntentReviewOptionText {
		add("recommended must be non-empty and at most %d bytes", MaxIntentReviewOptionText)
	}
	if f.Rank < 1 {
		add("rank must be at least 1")
	}
	if len(f.Rationale) > MaxIntentReviewText {
		add("rationale must be at most %d bytes", MaxIntentReviewText)
	}
	return errors.Join(errs...)
}

// AsQuestion returns the intent question gip records for the finding:
// class intent, the first span, the options and recommendation, the
// recommendation as the default of a non-blocking finding, and refs
// IntentReviewRef and "kind:<kind>".
func (f *IntentReviewFinding) AsQuestion() PlanQuestion {
	q := PlanQuestion{
		Text:        f.Question,
		Class:       QuestionClassIntent,
		Options:     append([]string(nil), f.Options...),
		Recommended: f.Recommended,
		Blocking:    f.Blocking,
		Refs:        []string{IntentReviewRef, IntentReviewKindRefPrefix + f.Kind},
	}
	if len(f.Spans) > 0 {
		q.Span = f.Spans[0]
	}
	if !f.Blocking {
		q.Default = f.Recommended
	}
	return q
}

// IntentReviewResult is the intent.review command's output. An empty
// Findings list means the review found nothing new.
type IntentReviewResult struct {
	Findings []IntentReviewFinding `json:"findings"`
	// Reviewers are the reviewer runs (planner name and model) that
	// produced the findings, when the plugin reports them.
	Reviewers []PlanSource `json:"reviewers,omitempty"`
}

// Validate checks the result against the request it answers: at most
// MaxQuestions findings, each valid against the content, with unique
// ranks.
func (r *IntentReviewResult) Validate(req *IntentReviewRequest) error {
	var errs []error
	if r.Findings == nil {
		errs = append(errs, errors.New("intent review result: findings must be present (empty when nothing was found)"))
	}
	if req.MaxQuestions > 0 && len(r.Findings) > req.MaxQuestions {
		errs = append(errs, fmt.Errorf("intent review result: %d findings exceed max_questions %d", len(r.Findings), req.MaxQuestions))
	}
	ranks := map[int]bool{}
	for i := range r.Findings {
		f := &r.Findings[i]
		if err := f.Validate(req.Content); err != nil {
			errs = append(errs, fmt.Errorf("intent review result: finding %d: %w", i, err))
		}
		if ranks[f.Rank] {
			errs = append(errs, fmt.Errorf("intent review result: rank %d given twice", f.Rank))
		}
		ranks[f.Rank] = true
	}
	return errors.Join(errs...)
}

// validateRoles checks the command roles of a manifest: each role is
// known, sits on a leaf command, needs its capability, is given to at
// most one command, and has its role's shape; and a plugin declaring
// CapReviewIntent gives some command role RoleIntentReview.
func (m *Manifest) validateRoles() []error {
	var errs []error
	byRole := map[string][]string{}
	var walk func(prefix []string, cs []Command)
	walk = func(prefix []string, cs []Command) {
		for i := range cs {
			c := &cs[i]
			p := append(append([]string{}, prefix...), c.Name)
			name := strings.Join(p, " ")
			if c.Role != "" {
				byRole[c.Role] = append(byRole[c.Role], name)
				switch {
				case !contains(KnownCommandRoles, c.Role):
					errs = append(errs, fmt.Errorf("command %q: role %q is unknown to protocol v%d", name, c.Role, Version))
				case len(c.Subcommands) > 0:
					errs = append(errs, fmt.Errorf("command %q: a command group cannot have a role", name))
				default:
					if capName := roleCapability[c.Role]; !contains(m.Capabilities, capName) {
						errs = append(errs, fmt.Errorf("command %q: role %q requires capability %q", name, c.Role, capName))
					}
					if err := CheckRoleShape(c); err != nil {
						errs = append(errs, fmt.Errorf("command %q: %w", name, err))
					}
				}
			}
			walk(p, c.Subcommands)
		}
	}
	walk(nil, m.Commands)
	for role, names := range byRole {
		if len(names) > 1 {
			errs = append(errs, fmt.Errorf("role %q is given to more than one command: %v", role, names))
		}
	}
	if contains(m.Capabilities, CapReviewIntent) && len(byRole[RoleIntentReview]) == 0 {
		errs = append(errs, fmt.Errorf("capability %q requires a command with role %q", CapReviewIntent, RoleIntentReview))
	}
	return errs
}

// CheckRoleShape checks that a leaf command has the shape its role
// requires. For RoleIntentReview: a required string argument
// IntentReviewRequestArg and no other required argument.
func CheckRoleShape(c *Command) error {
	switch c.Role {
	case "":
		return nil
	case RoleIntentReview:
		found := false
		for _, a := range c.Arguments {
			if a.Name == IntentReviewRequestArg {
				if a.Type != ArgString || !a.Required || a.Repeated {
					return fmt.Errorf("role %q: argument %q must be a required, single string", c.Role, IntentReviewRequestArg)
				}
				found = true
			} else if a.Required {
				return fmt.Errorf("role %q: argument %q must not be required; gip sends only %q", c.Role, a.Name, IntentReviewRequestArg)
			}
		}
		if !found {
			return fmt.Errorf("role %q: the command must declare a required string argument %q", c.Role, IntentReviewRequestArg)
		}
		return nil
	default:
		return fmt.Errorf("role %q is unknown to protocol v%d", c.Role, Version)
	}
}

// CommandForRole returns the path of the leaf command with the role, or
// nil.
func (m *Manifest) CommandForRole(role string) []string {
	for _, leaf := range m.Leaves() {
		if c := m.Find(leaf); c != nil && c.Role == role {
			return leaf
		}
	}
	return nil
}
