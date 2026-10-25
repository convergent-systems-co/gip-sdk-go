package protocol

import "time"

// Plan-once frontier messages (additive in SDK v0.4.0; gip ADR-0021).
//
// gip keeps a plan: a containment tree (root A = the intent's outcome,
// first-level branches B, C, ..., then B1, B2.1, B2.1.1 ...) plus
// dependency edges that may cross branches. Addresses never change and
// are never reused. The plan document is a projection of the ledger that
// gip updates incrementally; an executor reads it each round instead of
// recomputing the frontier, and still claims every unit through host/act.
// The plan never authorizes anything.
const (
	// MethodHostPlan reads the plan document (work.read). Passing the
	// digest the caller already holds returns Unchanged without the
	// document when nothing changed.
	MethodHostPlan = "host/plan"
	// MethodHostFinding records discovered work against a plan node, as
	// an agent principal the plugin was trusted to act as (work.execute).
	MethodHostFinding = "host/finding"
	// MethodHostPlanPass submits one planning pass, as an agent principal
	// the plugin was trusted to act as (plan.write).
	MethodHostPlanPass = "host/plan/pass"
)

// PermPlanWrite lets a planner submit planning passes through
// host/plan/pass (SDK v0.4.0). The pass is still decided by the gateway
// under the acting principal's own grants (plan.pass, goal.admit,
// work.admit, ...).
const PermPlanWrite = "plan.write"

// PlanSchema identifies the plan document.
const PlanSchema = "gip.plan-document/v1"

// Plan node statuses.
const (
	NodeReady      = "ready"
	NodeInFlight   = "in_flight"
	NodeWaiting    = "waiting"
	NodeBlocked    = "blocked"
	NodePlanned    = "planned"
	NodeOpen       = "open"
	NodeComplete   = "complete"
	NodeSuperseded = "superseded"
	NodeCancelled  = "cancelled"
)

// Node kinds.
const (
	KindRoot    = "root"
	KindGoal    = "goal"
	KindSubGoal = "sub-goal"
	KindWU      = "wu"
)

// Milestone statuses: complete and stuck are terminal.
const (
	MilestoneComplete = "complete"
	MilestoneActive   = "active"
	MilestoneWaiting  = "waiting"
	MilestoneStuck    = "stuck"
)

// Pass triggers.
const (
	TriggerInitial    = "initial"
	TriggerMilestone  = "milestone"
	TriggerEarly      = "early-milestone"
	TriggerBlocking   = "blocking"
	TriggerFinal      = "final"
	TriggerAdopt      = "adopt"
	TriggerBackground = "background"
	TriggerIdle       = "idle"
	// TriggerRescope (SDK v0.9.0, gip ADR-0024 §8): a rescope or split
	// finding made a re-scope of one unit due at once, scoped to the
	// unit's parent. The pass may supersede that unit (Replaces) even
	// though its work has started.
	TriggerRescope = "rescope"
	// TriggerRefine (SDK v0.9.0, gip ADR-0024 §6): an open question has
	// no pending intent proposal covering it; the planner answers it with
	// host/intent/propose, not with a pass.
	TriggerRefine = "refine"
)

// Finding statuses.
const (
	FindingOpen      = "open"
	FindingScheduled = "scheduled"
	FindingDismissed = "dismissed"
	// FindingDecided (SDK v0.5.0): an owner decision a human has decided.
	FindingDecided = "decided"
)

// HostPlanParams asks for the plan document. IfDigest: the digest of the
// document the caller holds.
//
// IdleSlots is an executor's report of the slots it could not fill: nil
// reports nothing (a planner, or a reader), a positive count reports
// idle slots, and 0 withdraws an earlier report. While a report stands,
// gip lists idle planning as due on branches with nothing in flight
// (TriggerIdle), so a planner can discover work for those slots. A gip
// that predates the field ignores it.
type HostPlanParams struct {
	IfDigest  string `json:"if_digest,omitempty"`
	IdleSlots *int   `json:"idle_slots,omitempty"`
}

// HostPlanResult carries the plan document, or Unchanged when it still
// has the digest the caller named. A project that was never planned has a
// document with Planned false and an initial trigger.
type HostPlanResult struct {
	Unchanged bool          `json:"unchanged,omitempty"`
	Digest    string        `json:"digest"`
	StateHead int64         `json:"state_head"`
	Plan      *PlanDocument `json:"plan,omitempty"`
}

// Reason explains a status with a stable code.
type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PlanEdge is a dependency of a node: Origin "plan" (added by a pass),
// "work" or "goal" (the bound object's depends_on).
type PlanEdge struct {
	To     string `json:"to"`
	Origin string `json:"origin"`
	Cross  bool   `json:"cross_branch"`
	Met    bool   `json:"met"`
}

// PlanWork is a work leaf's execution as gip's frontier classifies it.
type PlanWork struct {
	Execution  string   `json:"execution"`
	Executor   string   `json:"executor,omitempty"`
	Frontier   string   `json:"frontier_status"`
	NextAction string   `json:"next_action,omitempty"`
	Lease      *Lease   `json:"lease,omitempty"`
	Reasons    []Reason `json:"reasons"`
	// Design (v0.7.1) is the design revision the unit was started on and
	// is built and reviewed to; the document's Design is the current one.
	// Absent for a unit not started, or started before the plan had a
	// design.
	Design *DesignPin `json:"design,omitempty"`
	// CompletedSeq and CompletedPass (SDK v0.9.0, gip PLAN-036): for a
	// complete unit, the ledger seq of the event that completed it (its
	// qualification, or its integration when it was qualified at a
	// commit) and the latest planning pass at that seq. A planner compares
	// them with a finding's RecordedSeq to tell work discovered after the
	// unit was done from work the unit was asked to do. After a ledger
	// merge seq orders events along one line of history only. Absent while
	// the unit is not complete.
	CompletedSeq  int64 `json:"completed_seq,omitempty"`
	CompletedPass int   `json:"completed_pass,omitempty"`
}

// DesignPin identifies one revision of the plan's design by its number
// and the digest of its content (after a ledger merge two branches may
// each hold a different text under the same number).
type DesignPin struct {
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
}

// PlanNode is one node of the plan document. Kind is root, goal (first
// level), sub-goal or wu: every leaf is a work unit (WU) and only WUs are
// electable. Ref is the bound project, goal or work id. Rank is a WU's
// place in the election order fixed at planning time (1 first).
type PlanNode struct {
	Address    string       `json:"address"`
	Parent     string       `json:"parent,omitempty"`
	Branch     string       `json:"branch,omitempty"`
	Depth      int          `json:"depth"`
	Label      string       `json:"label"`
	Kind       string       `json:"kind"`
	Ref        string       `json:"ref"`
	Pass       int          `json:"pass"`
	Lifecycle  string       `json:"lifecycle"`
	Status     string       `json:"status"`
	Reasons    []Reason     `json:"reasons"`
	Rank       int          `json:"rank,omitempty"`
	Unblocks   int          `json:"unblocks,omitempty"`
	Chain      int          `json:"chain,omitempty"`
	DependsOn  []PlanEdge   `json:"depends_on"`
	Children   []string     `json:"children"`
	Findings   []string     `json:"findings"`
	Replaces   string       `json:"replaces,omitempty"`
	ReplacedBy string       `json:"replaced_by,omitempty"`
	Sources    []PlanSource `json:"sources,omitempty"`
	Confidence string       `json:"confidence,omitempty"`
	Work       *PlanWork    `json:"work,omitempty"`
	// Rescopes (SDK v0.9.0, gip ADR-0024 §8) counts the re-scopes and
	// splits of the node's unit lineage: a replacement counts those of the
	// units it replaces. One is allowed; a rescope or split finding on a
	// lineage already re-scoped becomes a blocking question instead.
	Rescopes int `json:"rescopes,omitempty"`
}

// PlanSource is one planner run (and its model) that produced a node.
type PlanSource struct {
	Planner string `json:"planner"`
	Model   string `json:"model,omitempty"`
}

// Confidence of a node two independent planners planned (initial and final
// discovery run two models; milestone and blocking passes one).
const (
	ConfidenceAgreed     = "agreed"
	ConfidenceSingle     = "single-source"
	ConfidenceReconciled = "reconciled"
)

// Milestone is the root's or a first-level branch's state. Planned: a
// pass already planned it at its current epoch.
type Milestone struct {
	Branch    string `json:"branch"`
	Status    string `json:"status"`
	Remaining int    `json:"remaining"`
	Findings  int    `json:"findings"`
	Epoch     int64  `json:"epoch"`
	Planned   bool   `json:"planned"`
}

// PlanFinding is discovered work recorded against a node.
type PlanFinding struct {
	ID       string `json:"id"`
	Node     string `json:"node"`
	Branch   string `json:"branch,omitempty"`
	Title    string `json:"title"`
	Blocking bool   `json:"blocking"`
	// Decision (SDK v0.5.0): the finding is an open owner decision, not
	// work. No planning pass schedules, counts or dismisses it; it waits
	// for a human (gip plan decide). Rules 1 only: under rules 2 a
	// decision is recorded as a question (gip ADR-0024 §6).
	Decision bool     `json:"decision,omitempty"`
	Status   string   `json:"status"`
	Pass     int      `json:"pass,omitempty"`
	Nodes    []string `json:"nodes,omitempty"`
	// Kind (SDK v0.9.0, gip ADR-0024 D7): FindingKindWork (absent),
	// FindingKindDecision, FindingKindRescope or FindingKindSplit. Read it
	// with EffectiveKind, which also honours the older Decision flag.
	Kind string `json:"kind,omitempty"`
	// RecordedSeq and RecordedPass (SDK v0.9.0, gip PLAN-036): the ledger
	// seq of the plan.find that recorded the finding and the latest
	// planning pass at that seq. Compare with PlanWork.CompletedSeq for
	// stale-finding triage.
	RecordedSeq  int64 `json:"recorded_seq,omitempty"`
	RecordedPass int   `json:"recorded_pass,omitempty"`
	// Provides (SDK v0.12.0, governed-intent-programming #147): for a
	// FindingKindPrerequisite finding, a one-line name of the seam,
	// protocol, package or capability the prerequisite supplies. Used to
	// match an existing work unit before deriving a new one.
	Provides string `json:"provides,omitempty"`
}

// EffectiveKind returns the finding's kind: Kind when set, otherwise
// FindingKindDecision for a finding with the Decision flag (records made
// before kinds), otherwise FindingKindWork.
func (f *PlanFinding) EffectiveKind() string {
	switch {
	case f.Kind != "":
		return f.Kind
	case f.Decision:
		return FindingKindDecision
	}
	return FindingKindWork
}

// Trigger is planning that is due: Kind is a pass trigger, Scope the
// address a pass for it must name.
type Trigger struct {
	Kind     string   `json:"kind"`
	Scope    string   `json:"scope"`
	Reason   string   `json:"reason"`
	Findings []string `json:"findings,omitempty"`
	Objects  []string `json:"objects,omitempty"`
}

// PlanDocument is gip's plan projection (gip.plan-document/v1). Ready
// lists ready work leaves in election order. Digest covers everything but
// StateHead and EvaluatedAt.
type PlanDocument struct {
	Schema      string         `json:"schema"`
	Algorithm   string         `json:"algorithm"`
	ProjectID   string         `json:"project_id"`
	StateHead   int64          `json:"state_head"`
	Pass        int            `json:"pass"`
	Planned     bool           `json:"planned"`
	Planners    []PlanSource   `json:"planners,omitempty"`
	SingleModel bool           `json:"single_model,omitempty"`
	Root        string         `json:"root,omitempty"`
	EvaluatedAt time.Time      `json:"evaluated_at"`
	Ready       []string       `json:"ready"`
	InFlight    []string       `json:"in_flight"`
	Counts      map[string]int `json:"counts"`
	Milestones  []Milestone    `json:"milestones"`
	Triggers    []Trigger      `json:"triggers"`
	Findings    []PlanFinding  `json:"findings"`
	Unplanned   []string       `json:"unplanned"`
	Nodes       []PlanNode     `json:"nodes"`
	// Design (SDK v0.7.0) is the plan's current design document, absent
	// until a pass sets one.
	Design *PlanDesign `json:"design,omitempty"`
	// Delivery (SDK v0.7.0) is present once the final discovery ran for
	// the current intent revision: it fires once per revision, and the
	// open findings it lists wait for the human at delivery.
	Delivery *PlanDelivery `json:"delivery,omitempty"`
	// Autonomous (SDK v0.9.0, gip ADR-0024 D5) is the project's setting:
	// true holds questions until the project is quiescent, false asks
	// them once the planner's refine pass ran. Absent from a gip that
	// predates it; absent in the project record means true.
	Autonomous *bool `json:"autonomous,omitempty"`
	// Intents (SDK v0.9.0) are the project's current intent revisions,
	// content included: the intent the planner recalls in every planning
	// prompt (gip ADR-0024 D4).
	Intents []PlanIntent `json:"intents,omitempty"`
	// Questions (SDK v0.9.0) are the open questions (gip ADR-0024 §6).
	Questions []PlanQuestion `json:"questions,omitempty"`
	// HumanDue (SDK v0.9.0) says whether the person is due to be asked.
	// Executors stop electing while it is due, and report it.
	HumanDue *HumanDue `json:"human_due,omitempty"`
	// Rules (SDK v0.12.1, gip#59) is the project's rules epoch (1, 2, 3).
	// Absent (0) from a gip that does not emit it: fall back to the text
	// of HumanDue.Reason only then.
	Rules int `json:"rules,omitempty"`
	// In-pass questions are supported when Rules >= 2. (No separate bool:
	// the json name "questions" is the open-question list above.)
	Digest string `json:"digest"`
}

// MaxDesignBytes bounds a plan design document (SDK v0.7.0): 64 KiB of
// UTF-8 markdown.
const MaxDesignBytes = 64 << 10

// PlanDesign is the plan's versioned design document (SDK v0.7.0; gip
// ADR-0021 amendment "front-loaded planning"): markdown describing the
// shared architecture, the modules, and the exact interface contracts
// (routes, request and response JSON shapes) consistent with the goals'
// outcome checks. Builders and reviewers build to it. Revision counts
// from 1; Pass is the planning pass that set it; Digest is the sha256 of
// Content ("sha256:<hex>"). Superseded revisions stay in gip's ledger.
type PlanDesign struct {
	Revision int          `json:"revision"`
	Pass     int          `json:"pass"`
	Digest   string       `json:"digest"`
	Content  string       `json:"content"`
	Planners []PlanSource `json:"planners,omitempty"`
}

// PlanDelivery records that the final discovery ran for the current
// intent revision (SDK v0.7.0). Findings are the open findings left for
// the human at delivery: no further final discovery schedules them.
// Record (SDK v0.9.0, gip ADR-0024 §7) is the delivery record for the
// current intent revisions once a reviewer prepared it: its goals,
// checks, held questions and status.
type PlanDelivery struct {
	Pass     int             `json:"pass"`
	Findings []string        `json:"findings"`
	Record   *DeliveryRecord `json:"record,omitempty"`
}

// Node returns the node at address a.
func (d *PlanDocument) Node(a string) (*PlanNode, bool) {
	for i := range d.Nodes {
		if d.Nodes[i].Address == a {
			return &d.Nodes[i], true
		}
	}
	return nil, false
}

// HostFindingParams records a finding against a node (an address) or the
// live node of a work unit, as principal As. A blocking finding stops the
// unit: only its current holder (or a principal granted plan.find) may
// record one, and gip then plans from that node's goal down. A Decision
// finding (SDK v0.5.0) is an open owner decision rather than work: gip
// routes it to a human and never plans it as work.
//
// Kind (SDK v0.9.0, gip ADR-0024 D7) types the finding: FindingKindWork
// (the default when absent), FindingKindDecision (the same as Decision;
// under rules 2 gip records a question at the node instead of a
// finding, and the result names it), FindingKindRescope or
// FindingKindSplit. A rescope or split finding names a work unit and is
// recorded by a builder of the unit, by its reviewer (a current
// work.review grant covering it) or by a principal granted plan.find;
// rescope also needs the unit's review rejections (ContractReview) to
// have reached the cap. Either makes a rescope pass due at once. A
// finding needs the work.execute plugin permission; one of kind rescope
// or split may instead be recorded under work.review, so a reviewer
// plugin needs no work.execute to send a unit back to planning. A gip
// built against an earlier SDK drops Kind and records a work finding:
// check that the host supports kinds before relying on them.
//
// FindingKindPrerequisite (SDK v0.12.0, governed-intent-programming #147)
// reports missing prerequisite work: it is always blocking, never a
// decision, and the planner answers it with derived work units the node
// depends on. Provides names what the prerequisite supplies.
type HostFindingParams struct {
	Work     string `json:"work,omitempty"`
	Node     string `json:"node,omitempty"`
	As       string `json:"as"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
	Decision bool   `json:"decision,omitempty"`
	Kind     string `json:"kind,omitempty"`
	// Provides (SDK v0.12.0): see PlanFinding.Provides.
	Provides  string `json:"provides,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// HostFindingResult is the gateway's disposition and the finding's id.
// Question (SDK v0.9.0) is the question gip recorded instead of a finding:
// a decision under rules 2, or a rescope or split on a lineage already
// re-scoped once (gip ADR-0024 §8).
type HostFindingResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Finding     string   `json:"finding,omitempty"`
	Question    string   `json:"question,omitempty"`
	EventSeq    int64    `json:"event_seq"`
}

// PlanPass is one planning pass (gip's plan.pass payload). Trigger and
// Scope must name planning that is due (a Trigger of the document), except
// background and idle passes, which schedule no findings. ExpectedPass is
// the document's Pass the planner worked from.
type PlanPass struct {
	Trigger      string              `json:"trigger"`
	Scope        string              `json:"scope"`
	Strategy     string              `json:"strategy"`
	ExpectedPass int                 `json:"expected_pass"`
	Nodes        []PlanNodeFields    `json:"nodes,omitempty"`
	Edges        []PlanEdgeFields    `json:"edges,omitempty"`
	Cancel       []PlanRetire        `json:"cancel,omitempty"`
	Findings     []FindingResolution `json:"findings,omitempty"`
	// Planners are the independent planner runs this pass reconciles.
	// Initial and final passes with fewer than two are flagged single-model.
	Planners []PlanSource `json:"planners,omitempty"`
	// Design (SDK v0.7.0) sets or replaces the plan's design document
	// (markdown, at most MaxDesignBytes of UTF-8). Only initial,
	// milestone, early-milestone and final passes may set it; empty
	// leaves the current design unchanged. A gip that predates the field
	// refuses a pass that names it. Under rules 2 a design set by an
	// initial or final pass must hold exactly one RuntimeBlock (SDK
	// v0.9.0, gip ADR-0024 §7).
	Design string `json:"design,omitempty"`
	// Questions (SDK v0.9.0, gip ADR-0024 D4) are raised by the pass, in
	// the same event: each becomes an intent ambiguity carrying the
	// question shape. A dismissal on scope grounds must name one of them
	// by Ref (FindingResolution.Question). A gip that predates the field
	// refuses a pass that names it.
	Questions []PlanQuestion `json:"questions,omitempty"`
}

// PlanBinding names an existing goal or work unit (or, for the root, the
// project).
type PlanBinding struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PlanNodeFields is a new node: exactly one of Bind, Goal and Work. Parent
// and DependsOn are addresses or the Ref of an earlier node in the pass;
// gip assigns the address (the next number never used under the parent).
type PlanNodeFields struct {
	Ref       string        `json:"ref,omitempty"`
	Parent    string        `json:"parent"`
	Label     string        `json:"label,omitempty"`
	Bind      *PlanBinding  `json:"bind,omitempty"`
	Goal      *PlanGoal     `json:"goal,omitempty"`
	Work      *PlanWorkUnit `json:"work,omitempty"`
	DependsOn []string      `json:"depends_on,omitempty"`
	// Replaces supersedes a node: an address planned by an earlier pass,
	// or (gip PLAN-025, same-pass supersession) the Ref or address of a
	// node an earlier entry of this same pass added with Bind. A planner
	// can bind coarse work and replace it in the same pass with Work nodes
	// carrying refined objectives; gip records the bound node as added
	// and superseded and withdraws the bound object. A node the same pass
	// created (Goal or Work) cannot be superseded in it. Superseding needs
	// work.withdraw (goal.withdraw for goal nodes) under the planner's
	// grants. In a rescope pass (TriggerRescope) it may name the one unit
	// the rescope finding names even though its work has started.
	Replaces   string       `json:"replaces,omitempty"`
	Sources    []PlanSource `json:"sources,omitempty"`
	Confidence string       `json:"confidence,omitempty"`
}

// PlanGoal creates and admits a sub-goal (under the planner's goal.admit).
type PlanGoal struct {
	ID       string   `json:"id,omitempty"`
	Title    string   `json:"title"`
	Outcome  string   `json:"outcome"`
	Requires []string `json:"requires,omitempty"`
}

// PlanWorkUnit creates and admits a work unit (under work.admit).
type PlanWorkUnit struct {
	ID        string   `json:"id,omitempty"`
	Objective string   `json:"objective"`
	Requires  []string `json:"requires,omitempty"`
	Review    bool     `json:"review,omitempty"`
	Authority []string `json:"authority,omitempty"`
}

// PlanEdgeFields adds a dependency from a node in scope to any node.
type PlanEdgeFields struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PlanRetire cancels a node in scope that has no work started.
type PlanRetire struct {
	Node   string `json:"node"`
	Reason string `json:"reason"`
}

// FindingResolution schedules a finding as nodes of this pass, or
// dismisses it with a reason.
//
// Grounds and Question (SDK v0.9.0, gip ADR-0024 D4) qualify a dismissal:
// Grounds GroundsScope (the finding is outside the intent) must name, in
// Question, the Ref of a question raised in this same pass, so the
// dismissal and its question are one event (QUESTION_REQUIRED).
type FindingResolution struct {
	Finding  string   `json:"finding"`
	Nodes    []string `json:"nodes,omitempty"`
	Dismiss  string   `json:"dismiss,omitempty"`
	Grounds  string   `json:"grounds,omitempty"`
	Question string   `json:"question,omitempty"`
	// CoveredBy (SDK v0.12.1, gip-sdk-go#22) names the node that covers a
	// prerequisite finding dismissed as duplicate or done (core PLAN-039):
	// a node address or a ref of this pass. Omitted when empty. An older
	// gip refuses it (INVALID_PAYLOAD: unknown field).
	CoveredBy string `json:"covered_by,omitempty"`
}

// Dismissal grounds (SDK v0.9.0).
const (
	// GroundsScope: the finding is out of the intent's scope; the pass
	// raises the question whether to extend it.
	GroundsScope = "scope"
)

// HostPlanPassParams submits a pass as principal As.
type HostPlanPassParams struct {
	As        string   `json:"as"`
	RequestID string   `json:"request_id,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
	Pass      PlanPass `json:"pass"`
}

// HostPlanPassResult is the gateway's disposition. Added lists the new
// node addresses in payload order; Refs maps each payload ref to its
// address; Created lists the goals and work units admitted.
type HostPlanPassResult struct {
	Disposition string            `json:"disposition"`
	Code        string            `json:"code,omitempty"`
	Reasons     []string          `json:"reasons"`
	Ceremony    string            `json:"ceremony,omitempty"`
	EventSeq    int64             `json:"event_seq"`
	Pass        int               `json:"pass,omitempty"`
	Added       []string          `json:"added,omitempty"`
	Refs        map[string]string `json:"refs,omitempty"`
	Created     []string          `json:"created,omitempty"`
	// Omitted (SDK v0.9.0, gip ADR-0024 §4) lists the payload refs gip
	// left out: goals the intent does not contain, their subtrees and
	// every node of the pass depending on them. A class goal question is
	// raised for each such goal; the rest of the pass is applied.
	Omitted []string `json:"omitted,omitempty"`
	// Questions (SDK v0.9.0) maps each question ref of the pass (and each
	// goal question gip raised for an omitted goal, by the goal's ref) to
	// the id of the question recorded.
	Questions map[string]string `json:"questions,omitempty"`
}
