package protocol

import (
	"encoding/json"
	"slices"
	"time"
)

// Agent authority and human presence (additive in SDK v0.10.0; gip
// ADR-0025, batch C). The SDK carries the record shapes a plugin reads
// (the plan document's questions, provenance, merge-conflict ceremonies);
// gip decides and verifies everything. A gip that predates ADR-0025 never
// sends the new fields.

// RulesAgentAuthority is rules epoch 3 (gip ADR-0025 D1): the human-only
// list becomes intent.revise, delivery.accept and project.settings, and
// grant, identity, gateway, lease and conflict administration become
// grantable to agents (agent:* principals only), under delegation chains,
// conflict of interest and presence. It is not selectable until gip's
// switch-on release (core K); until then gip's maximum epoch is
// RulesDarkFactory.
const RulesAgentAuthority = 3

// HumanOnlyActions returns the acts reserved to a person under the given
// rules epoch, as gip's governance lists them (ADR-0024 §1, ADR-0025 D1) (for display
// and parity tests; gip's own list is authoritative, and once core pins
// this function in its parity test, that test owns it: a change to an
// epoch's list in core must change it here). Unknown epochs return nil.
func HumanOnlyActions(rules int) []string {
	switch rules {
	case RulesClassic:
		return []string{"goal.accept", "authority.grant", "authority.revoke", "identity.bind", "identity.revoke",
			"project.gateway", "ledger.resolve", "project.lease", "plan.decide", "project.settings"}
	case RulesAgentAuthority:
		return []string{"intent.revise", "delivery.accept", "project.settings"}
	case RulesDarkFactory:
		return []string{"intent.revise", "delivery.accept", "project.settings",
			"authority.grant", "authority.revoke", "identity.bind", "identity.revoke",
			"project.gateway", "ledger.resolve", "project.lease"}
	}
	return nil
}

// Presence is the human-presence signature on an attestation (gip
// ADR-0025 D8, §5): Key is the enrolled presence public key as recorded in
// the identity's presence key event (gip.identity/v2), SignedAt when the
// person touched, and Sig the signature over the presence payload. gip
// verifies it; a plugin only reads it.
type Presence struct {
	Key      string    `json:"key"`
	SignedAt time.Time `json:"signed_at"`
	Sig      string    `json:"sig"`
}

// Presence signing (ADR-0025 §5). The payload is canonical JSON prefixed
// with PresenceContext; its domain separates a ledger request from an
// out-of-ledger installation decision, so a signature in one never
// verifies in the other.
const (
	PresenceContext            = "gip/v1/presence"
	PresenceDomainLedger       = "ledger"
	PresenceDomainInstallation = "installation"
	// PresenceFreshness bounds |presence.signed_at - request.signed_at|
	// (PRESENCE_INVALID beyond it).
	PresenceFreshness = 5 * time.Minute
)

// Presence refusal codes (ADR-0025 D8).
const (
	CodePresenceRequired = "PRESENCE_REQUIRED"
	CodePresenceInvalid  = "PRESENCE_INVALID"
)

// CeremonyMergeConflict is the type of a ceremony that holds an object
// whose versions on two merged ledger branches conflict (gip ADR-0014).
const CeremonyMergeConflict = "merge-conflict"

// Merge-conflict classes (MergePending.Class, ADR-0025 D5).
const (
	// ConflictClassHuman: the conflicted object is human-class (the
	// intent, the project record, a delivery, a human's grants or binding);
	// only a person resolves it (HUMAN_CLASS_CONFLICT for an agent).
	ConflictClassHuman = "human"
	// ConflictClassAgent: an independent agent holding ledger.resolve may
	// resolve it.
	ConflictClassAgent = "agent"
)

// Conflict resolution refusal codes (ADR-0025 D4, D5).
const (
	CodeConflictOfInterest = "CONFLICT_OF_INTEREST"
	CodeAuthorsUnknown     = "AUTHORS_UNKNOWN"
	CodeHumanClassConflict = "HUMAN_CLASS_CONFLICT"
)

// MergeCandidate is one version of a conflicted object: Label names it
// (a, b, base or absent), Head the branch head it came from, Record the
// object as recorded there.
type MergeCandidate struct {
	Label  string          `json:"label"`
	Head   string          `json:"head,omitempty"`
	Record json.RawMessage `json:"record"`
}

// MergePending is the pending payload of a merge-conflict ceremony (gip
// ceremony.v1, ADR-0014 §3). Authors and Class (SDK v0.10.0, ADR-0025 §3)
// are recorded by merges under rules 3: Authors (sorted, deduplicated)
// are the principals conflicted over the object, whom gip refuses as
// resolvers (CONFLICT_OF_INTEREST), and Class says whether an agent may
// resolve it at all. A conflict recorded without Authors (before rules 3)
// needs a person (AUTHORS_UNKNOWN).
type MergePending struct {
	Parents     []string         `json:"parents"`
	Provisional string           `json:"provisional"`
	Candidates  []MergeCandidate `json:"candidates"`
	Authors     []string         `json:"authors,omitempty"`
	Class       string           `json:"class,omitempty"`
}

// AuthorsKnown reports whether the merge recorded its authors (a merge
// under rules 3 always records at least the flagged event's principal).
func (m *MergePending) AuthorsKnown() bool { return m != nil && len(m.Authors) > 0 }

// IsAuthor reports whether principal is among the conflict's authors. It
// is a convenience for display: gip's conflict-of-interest test also
// follows grant chains and binding chains (ADR-0025 D4), which a plugin
// cannot see.
func (m *MergePending) IsAuthor(principal string) bool {
	return m != nil && slices.Contains(m.Authors, principal)
}

// AgentResolvable reports whether an agent may resolve the conflict at
// all: authors recorded and class agent. Whether a given agent may is
// gip's decision.
func (m *MergePending) AgentResolvable() bool {
	return m.AuthorsKnown() && m.Class == ConflictClassAgent
}
