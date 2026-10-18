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
)

// Finding statuses.
const (
	FindingOpen      = "open"
	FindingScheduled = "scheduled"
	FindingDismissed = "dismissed"
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
	ID       string   `json:"id"`
	Node     string   `json:"node"`
	Branch   string   `json:"branch,omitempty"`
	Title    string   `json:"title"`
	Blocking bool     `json:"blocking"`
	Status   string   `json:"status"`
	Pass     int      `json:"pass,omitempty"`
	Nodes    []string `json:"nodes,omitempty"`
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
	Digest      string         `json:"digest"`
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
// record one, and gip then plans from that node's goal down.
type HostFindingParams struct {
	Work      string `json:"work,omitempty"`
	Node      string `json:"node,omitempty"`
	As        string `json:"as"`
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	Blocking  bool   `json:"blocking,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// HostFindingResult is the gateway's disposition and the finding's id.
type HostFindingResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Finding     string   `json:"finding,omitempty"`
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
	Ref        string        `json:"ref,omitempty"`
	Parent     string        `json:"parent"`
	Label      string        `json:"label,omitempty"`
	Bind       *PlanBinding  `json:"bind,omitempty"`
	Goal       *PlanGoal     `json:"goal,omitempty"`
	Work       *PlanWorkUnit `json:"work,omitempty"`
	DependsOn  []string      `json:"depends_on,omitempty"`
	Replaces   string        `json:"replaces,omitempty"`
	Sources    []PlanSource  `json:"sources,omitempty"`
	Confidence string        `json:"confidence,omitempty"`
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
type FindingResolution struct {
	Finding string   `json:"finding"`
	Nodes   []string `json:"nodes,omitempty"`
	Dismiss string   `json:"dismiss,omitempty"`
}

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
}
