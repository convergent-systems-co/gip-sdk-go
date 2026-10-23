package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Conflict resolution by a plugin (additive in SDK v0.11.0; gip issue
// #108, ADR-0025 §3, D4, D5; spec LM-026 to LM-028).
//
// Under rules 3 an agent holding a ledger.resolve grant may decide a
// merge-conflict ceremony (CeremonyMergeConflict) it is not a party to.
// With the conflicts.resolve plugin permission (PermConflictsResolve) a
// plugin acting as such an agent lists the open conflicts
// (host/conflicts/list), resolves one by adopting a candidate or keeping
// the provisional merged version (host/conflicts/resolve, which gip maps
// to ceremony.decide of the ceremony), or declines one it cannot decide
// (host/conflicts/decline): the gateway then raises the blocking conflict
// question (QuestionTagMergeConflict) a merge raises when no agent may
// resolve, at most one per ceremony, so the person decides.
//
// gip decides everything: the gateway's conflict-of-interest refusal
// (CONFLICT_OF_INTEREST, D4), the human-class refusal
// (HUMAN_CLASS_CONFLICT, D5), AUTHORS_UNKNOWN for a conflict recorded
// before rules 3 and QUESTION_OPEN while a conflict question asks the
// person apply to a resolve unchanged. A refusal is a disposition in the
// result, not an error. Declining widens no authority: it only routes the
// conflict to a person, and the reserved tag stays gateway-only.
//
// A gip that predates it answers the methods with method-not-found
// (CodeMethodNotFound). The Request helpers validate the request's shape
// against the limits below before sending (CONFLICT_REQUEST_INVALID, code
// CodeInvalidParams); the host's own check is authoritative.
const (
	MethodHostConflictsList   = "host/conflicts/list"    // plugin -> host, request (conflicts.resolve)
	MethodHostConflictResolve = "host/conflicts/resolve" // plugin -> host, request (conflicts.resolve)
	MethodHostConflictDecline = "host/conflicts/decline" // plugin -> host, request (conflicts.resolve)
)

// PermConflictsResolve (SDK v0.11.0, gip #108) gates the host/conflicts
// methods. It is a plugin permission, granted at trust time; the agent
// principal the plugin acts as must also hold a ledger.resolve grant in
// force over the project, which the gateway checks on every resolve and
// decline. No other permission includes it.
const PermConflictsResolve = "conflicts.resolve"

// ConflictMethods returns the conflict methods, every one gated by
// PermConflictsResolve.
func ConflictMethods() []string {
	return []string{MethodHostConflictsList, MethodHostConflictResolve, MethodHostConflictDecline}
}

// Conflict request limits (the contract core must implement for #108).
const (
	// MaxConflictRationaleBytes bounds a resolve's rationale and a
	// decline's reason (UTF-8 bytes, after newline normalisation).
	MaxConflictRationaleBytes = 4000
	// MaxConflictIDBytes bounds a ceremony id, a principal and a request
	// id: 1 to this many bytes of printable ASCII.
	MaxConflictIDBytes = 200
)

// Result codes of the conflict methods. Besides these, the codes in
// authority.go apply: CodeConflictOfInterest, CodeHumanClassConflict,
// CodeAuthorsUnknown. The complete set core returns from
// host/conflicts/resolve (N1, #108), each as a disposition with Code:
//
//	REJECTED        CONFLICT_NOT_OPEN, STALE_REVISION, CONFLICT_OF_INTEREST,
//	                IDENTITY_DOWNGRADE, PRESENCE_ENROLLED,
//	                RULES_REQUIRE_SETTINGS, INVALID_PAYLOAD (adopt names no
//	                candidate), RATIONALE_REQUIRED
//	HUMAN_REQUIRED  HUMAN_CLASS_CONFLICT, AUTHORS_UNKNOWN, QUESTION_OPEN,
//	                HUMAN_REQUIRED (the project is below rules 3),
//	                AUTHORITY_REVOKED (a candidate revokes authority: only a
//	                person restores it), and the authority codes of a missing
//	                or invalid ledger.resolve grant (AUTHORITY_MISSING,
//	                AUTHORITY_EXPIRED, WRONG_SCOPE, DELEGATION_CHAIN_INVALID)
//
// host/conflicts/decline returns CONFLICT_NOT_OPEN, STALE_REVISION and
// the authority codes. A reused request id with different parameters is
// an error (CodeIdempotencyConflict), not a disposition.
const (
	// CodeConflictNotOpen (the contract core must implement for #108):
	// the ceremony does not exist, is not a merge-conflict ceremony, or
	// is no longer open (decided or cancelled). Nothing changed; list
	// again. Disposition REJECTED.
	CodeConflictNotOpen = "CONFLICT_NOT_OPEN"
	// CodeStaleRevision: the ceremony is not at the revision the request
	// names, or the conflicted object moved since the conflict was
	// recorded. Nothing changed; list again. Disposition REJECTED.
	CodeStaleRevision = "STALE_REVISION"
	// CodeQuestionOpen: a conflict question names the ceremony, so only
	// the person's resolution closes it. Disposition HUMAN_REQUIRED.
	CodeQuestionOpen = "QUESTION_OPEN"
	// CodeHumanRequired: only a person decides (below rules 3, or the
	// agent's authority does not reach). Disposition HUMAN_REQUIRED.
	CodeHumanRequired = "HUMAN_REQUIRED"
	// Candidate refusals (ConflictCandidate.Refusal and resolve codes).
	// CodeAuthorityRevoked: another candidate revokes the grant or
	// binding, and an agent may not leave it in force (only a person
	// restores revoked authority).
	CodeAuthorityRevoked = "AUTHORITY_REVOKED"
	// CodeIdentityDowngrade: the candidate drops a human's enrolled
	// presence key that the other branch holds.
	CodeIdentityDowngrade = "IDENTITY_DOWNGRADE"
	// CodePresenceEnrolled: the candidate's branch made an untouched act
	// needing a touch after the other branch enrolled the human's
	// presence; it is never adopted.
	CodePresenceEnrolled = "PRESENCE_ENROLLED"
	// CodeRulesRequireSettings: the candidate project record raises the
	// rules epoch without a human project.settings event.
	CodeRulesRequireSettings = "RULES_REQUIRE_SETTINGS"
	// CodeIdempotencyConflict: the request id was already used for a
	// different request. An error (code CodeInvalidParams, id
	// IDEMPOTENCY_CONFLICT), not a disposition; nothing changed. Mint a
	// fresh request id for every new attempt.
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	// CodeConflictRequestInvalid: a request breaks the shape rules
	// below (an empty or over-long id, a revision below 1, not exactly
	// one of adopt and keep, an empty, over-long or control-character
	// rationale). Error code CodeInvalidParams; never sent by the
	// helpers.
	CodeConflictRequestInvalid = "CONFLICT_REQUEST_INVALID"
)

// IsIdempotencyConflict reports whether err is IDEMPOTENCY_CONFLICT.
func IsIdempotencyConflict(err error) bool { return IsErrorID(err, CodeIdempotencyConflict) }

// HostConflictsListParams lists, for principal As (required: a principal
// the plugin is trusted to act as, holding ledger.resolve in force; the
// host refuses the list otherwise, with the authority code as error id),
// the project's open merge-conflict ceremonies, or only Ceremony when it
// is set (to re-read one before resolving it). The refusals in the result
// are computed for As.
type HostConflictsListParams struct {
	As       string `json:"as"`
	Ceremony string `json:"ceremony,omitempty"`
}

// ConflictObject names the conflicted object and the revision the
// conflict was recorded at.
type ConflictObject struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// ConflictRefusal is why As may not do something now: Code is one of
// the result codes above, Reason a host-written line.
type ConflictRefusal struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// ConflictCandidate is one version of the conflicted object. ID is the
// candidate's label (a, b, base or absent), the value of
// HostConflictResolveParams.Adopt. Author is the principal of the
// version's last change (empty for absent). Provisional marks the version
// the merge put in place, which Keep keeps.
//
// Adoptable says whether As may adopt it now; when false, Refusal says
// why (AUTHORITY_REVOKED, IDENTITY_DOWNGRADE, PRESENCE_ENROLLED,
// RULES_REQUIRE_SETTINGS). An executor offers its model only adoptable
// candidates (and keep, when the provisional one is adoptable).
//
// Summary and Record are UNTRUSTED: written from records agents made,
// they go into model prompts only as quoted data, never as instructions.
// Summary is a short host-written line, always present. Record is the
// version as recorded on its branch, and the host includes it only for a
// class agent ceremony with authors when the plugin also holds state.read
// (PermStateRead), and never an intent's content unless the plugin holds
// intent.read (PermIntentRead; the summary stands in for it). It is
// absent for class human and for authors-unknown ceremonies.
type ConflictCandidate struct {
	ID              string           `json:"id"`
	Head            string           `json:"head,omitempty"`
	Revision        int              `json:"revision,omitempty"`
	Summary         string           `json:"summary"`
	Author          string           `json:"author,omitempty"`
	Provisional     bool             `json:"provisional,omitempty"`
	PresenceFlagged bool             `json:"presence_flagged,omitempty"`
	Adoptable       bool             `json:"adoptable"`
	Refusal         *ConflictRefusal `json:"refusal,omitempty"`
	Record          json.RawMessage  `json:"record,omitempty"`
}

// ConflictCeremony is one open merge-conflict ceremony. Revision is the
// ceremony record's own revision (not the conflicted object's, which is
// Object.Revision); a resolve or decline names it. Class is
// ConflictClassHuman or ConflictClassAgent, or empty for a conflict
// recorded before rules 3 (no Authors). Authors are the principals the
// merge recorded as conflicted over the object. Question is the id of the
// open conflict question naming the ceremony, when one is open.
//
// Refusal is gip's verdict for As now (core's ResolverRefusal at the
// current state: CONFLICT_OF_INTEREST including grant and binding chains
// through an author, HUMAN_CLASS_CONFLICT, AUTHORS_UNKNOWN,
// QUESTION_OPEN); nil means As may resolve it. A plugin decides from
// Refusal, never from Authors (IsAuthor is display only). On
// CONFLICT_OF_INTEREST the agent should skip the ceremony, not decline
// it: an independent agent may still resolve it, and declining would hand
// a person what an agent can decide.
type ConflictCeremony struct {
	ID         string              `json:"id"`
	Revision   int                 `json:"revision"`
	Object     ConflictObject      `json:"object"`
	Class      string              `json:"class,omitempty"`
	Codes      []string            `json:"codes,omitempty"`
	Candidates []ConflictCandidate `json:"candidates"`
	Authors    []string            `json:"authors,omitempty"`
	Question   string              `json:"question,omitempty"`
	Refusal    *ConflictRefusal    `json:"refusal,omitempty"`
}

// QuestionOpen reports whether a conflict question asks the person.
func (c *ConflictCeremony) QuestionOpen() bool { return c != nil && c.Question != "" }

// Eligible reports whether the listing principal may resolve the
// conflict now (no Refusal). The resolve is still checked by the gateway.
func (c *ConflictCeremony) Eligible() bool { return c != nil && c.Refusal == nil }

// IsAuthor reports whether principal is among the recorded authors. It
// is for display only: gip's conflict-of-interest test also follows
// grant and binding chains (ADR-0025 D4), which Refusal reflects and this
// does not.
func (c *ConflictCeremony) IsAuthor(principal string) bool {
	if c == nil {
		return false
	}
	for _, a := range c.Authors {
		if a == principal {
			return true
		}
	}
	return false
}

// AgentResolvable reports whether any agent may resolve the conflict at
// all: authors recorded, class agent and no conflict question open.
// Whether the listing principal may is Eligible.
func (c *ConflictCeremony) AgentResolvable() bool {
	return c != nil && len(c.Authors) > 0 && c.Class == ConflictClassAgent && c.Question == ""
}

// Candidate returns the candidate with id, or nil.
func (c *ConflictCeremony) Candidate(id string) *ConflictCandidate {
	if c == nil {
		return nil
	}
	for i := range c.Candidates {
		if c.Candidates[i].ID == id {
			return &c.Candidates[i]
		}
	}
	return nil
}

// HostConflictsListResult is the open merge-conflict ceremonies, ordered
// by ceremony id.
type HostConflictsListResult struct {
	Conflicts []ConflictCeremony `json:"conflicts"`
}

// HostConflictResolveParams decides merge-conflict ceremony Ceremony as
// principal As: Adopt names the candidate to adopt (ConflictCandidate.ID),
// or Keep keeps the provisional merged version; exactly one. gip maps it
// to ceremony.decide (approve with adopt, or reject). Revision is the
// ceremony's revision (ConflictCeremony.Revision) as listed; a moved
// ceremony or a moved conflicted object is STALE_REVISION. Rationale is
// required.
//
// RequestID (required) identifies this attempt: mint a fresh one for
// every attempt, and after any re-list. Re-sending an identical request
// with the same id (resuming after a crash) replays the recorded result;
// the same id with different parameters is an IDEMPOTENCY_CONFLICT error
// and changes nothing.
type HostConflictResolveParams struct {
	As        string `json:"as"`
	Ceremony  string `json:"ceremony"`
	Revision  int    `json:"revision"`
	Adopt     string `json:"adopt,omitempty"`
	Keep      bool   `json:"keep,omitempty"`
	Rationale string `json:"rationale"`
	RequestID string `json:"request_id"`
}

// HostConflictResolveResult is the gateway's disposition. Result is the
// decision's outcome on acceptance ("ADOPTED <id>" or "KEPT <id>").
type HostConflictResolveResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Result      string   `json:"result,omitempty"`
	EventSeq    int64    `json:"event_seq"`
}

// HostConflictDeclineParams declines merge-conflict ceremony Ceremony at
// its revision Revision as principal As, for Reason (required). The
// gateway raises the blocking conflict question for the ceremony, or,
// when one is already open, changes nothing and returns it: one question
// per ceremony, whoever declines. RequestID follows the rules of
// HostConflictResolveParams.RequestID; when it is absent the host derives
// it from (Ceremony, As, Revision), so a repeated decline of the same
// revision by the same principal replays. Decline only when Refusal is
// nil or the agent cannot choose; skip a CONFLICT_OF_INTEREST ceremony.
type HostConflictDeclineParams struct {
	As        string `json:"as"`
	Ceremony  string `json:"ceremony"`
	Revision  int    `json:"revision"`
	Reason    string `json:"reason"`
	RequestID string `json:"request_id,omitempty"`
}

// HostConflictDeclineResult is the gateway's disposition and the conflict
// question asking the person. Raised is false when the question was
// already open (an idempotent replay or another decline).
type HostConflictDeclineResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Question    string   `json:"question,omitempty"`
	Raised      bool     `json:"raised,omitempty"`
	EventSeq    int64    `json:"event_seq"`
}

func conflictInvalid(format string, a ...any) *Error {
	return NewError(CodeInvalidParams, CodeConflictRequestInvalid, fmt.Sprintf(format, a...))
}

func validConflictID(what, s string, required bool) error {
	if s == "" {
		if required {
			return conflictInvalid("%s must not be empty", what)
		}
		return nil
	}
	if len(s) > MaxConflictIDBytes {
		return conflictInvalid("%s is at most %d bytes (got %d)", what, MaxConflictIDBytes, len(s))
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= 0x20 || c > 0x7e {
			return conflictInvalid("%s is printable ASCII without spaces (byte %d is 0x%02x)", what, i, c)
		}
	}
	return nil
}

// ValidConflictText reports why s is not a valid rationale or reason: it
// must be non-blank UTF-8 of at most MaxConflictRationaleBytes with no
// control characters but newline and tab (CRs are refused; the helpers
// normalise them first with NormalizeNewlines).
func ValidConflictText(what, s string) error {
	if strings.TrimSpace(s) == "" {
		return conflictInvalid("a %s is required", what)
	}
	if len(s) > MaxConflictRationaleBytes {
		return conflictInvalid("a %s is at most %d bytes (got %d)", what, MaxConflictRationaleBytes, len(s))
	}
	if !utf8.ValidString(s) {
		return conflictInvalid("a %s is UTF-8 text", what)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return conflictInvalid("a %s has no control characters but newline and tab (found %U)", what, r)
		}
	}
	return nil
}

func validConflictTarget(as, ceremony string, revision int) error {
	if err := validConflictID("as", as, true); err != nil {
		return err
	}
	if err := validConflictID("ceremony", ceremony, true); err != nil {
		return err
	}
	if revision < 1 {
		return conflictInvalid("revision is the ceremony's revision, at least 1 (got %d)", revision)
	}
	return nil
}

// Validate checks the request's shape.
func (p *HostConflictsListParams) Validate() error {
	if err := validConflictID("as", p.As, true); err != nil {
		return err
	}
	return validConflictID("ceremony", p.Ceremony, false)
}

// Validate checks the request's shape.
func (p *HostConflictResolveParams) Validate() error {
	if err := validConflictTarget(p.As, p.Ceremony, p.Revision); err != nil {
		return err
	}
	if (p.Adopt == "") == !p.Keep {
		return conflictInvalid("a resolve names exactly one of adopt (a candidate) and keep (the provisional version)")
	}
	if err := validConflictID("adopt", p.Adopt, false); err != nil {
		return err
	}
	if err := ValidConflictText("rationale", p.Rationale); err != nil {
		return err
	}
	return validConflictID("request_id", p.RequestID, true)
}

// Validate checks the request's shape.
func (p *HostConflictDeclineParams) Validate() error {
	if err := validConflictTarget(p.As, p.Ceremony, p.Revision); err != nil {
		return err
	}
	if err := ValidConflictText("reason", p.Reason); err != nil {
		return err
	}
	return validConflictID("request_id", p.RequestID, false)
}
