package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Transport: JSON-RPC 2.0, one JSON object per line (NDJSON) on the plugin's
// stdin (host -> plugin) and stdout (plugin -> host). Stderr is free-form
// diagnostics for the human and is never parsed.

// MaxMessageBytes bounds a single protocol line.
const MaxMessageBytes = 8 << 20

// Methods.
const (
	MethodInitialize = "initialize"     // host -> plugin, request
	MethodInvoke     = "command/invoke" // host -> plugin, request
	MethodShutdown   = "shutdown"       // host -> plugin, notification

	MethodTelemetry  = "telemetry/emit" // plugin -> host, notification
	MethodHostIntent = "host/intent"    // plugin -> host, request (intent.read)
	MethodHostState  = "host/state"     // plugin -> host, request (state.read)

	// Executor methods (protocol v1, additive in SDK v0.2.0). An executor
	// plugin reads the frontier and work contracts, and acts on work only
	// as an agent principal the human named when trusting it (--act-as).
	MethodHostFrontier = "host/frontier" // plugin -> host, request (work.read)
	MethodHostContract = "host/contract" // plugin -> host, request (work.read)
	MethodHostAct      = "host/act"      // plugin -> host, request (work.execute)
	MethodHostCheck    = "host/check"    // plugin -> host, request (work.execute)
	MethodHostConfig   = "host/config"   // plugin -> host, request (config.read)

	// Lease and progress methods (additive in SDK v0.3.0, ADR-0015).
	// host/progress reports what a unit the executor holds is doing; it
	// is telemetry, never a governed transition. host/execution reads
	// the leases and latest progress of units in flight.
	MethodHostProgress  = "host/progress"  // plugin -> host, request (work.execute)
	MethodHostExecution = "host/execution" // plugin -> host, request (work.read)
)

// Error codes. -32xxx are JSON-RPC; -31xxx are gip-specific.
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternal         = -32603
	CodePermissionDenied = -31001
	CodeIncompatible     = -31002
	CodeCommandFailed    = -31003
)

// Message is a JSON-RPC 2.0 request, notification or response.
type Message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// Error is a structured protocol error. ID is a stable error identifier
// (for example "discovery.no_intent") that callers may match on.
type Error struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Data    *ErrorData `json:"data,omitempty"`
}

// ErrorData carries the stable identifier and optional detail.
type ErrorData struct {
	ID     string          `json:"id"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	if e.Data != nil && e.Data.ID != "" {
		return fmt.Sprintf("%s (%s, code %d)", e.Message, e.Data.ID, e.Code)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// IsRequest reports whether m is a request (has method and id).
func (m *Message) IsRequest() bool { return m.Method != "" && m.ID != nil }

// IsNotification reports whether m is a notification (method, no id).
func (m *Message) IsNotification() bool { return m.Method != "" && m.ID == nil }

// InitializeParams is sent by the host first.
type InitializeParams struct {
	ProtocolVersions []int  `json:"protocol_versions"`
	HostName         string `json:"host_name"`
	HostVersion      string `json:"host_version"`
	// GrantedPermissions are the permissions the trust policy granted. The
	// host enforces them; the plugin may use them to degrade gracefully.
	GrantedPermissions []string `json:"granted_permissions"`
}

// InitializeResult must restate the manifest identity. The host aborts
// when it disagrees with the manifest it discovered.
type InitializeResult struct {
	ProtocolVersion int    `json:"protocol_version"`
	Name            string `json:"name"`
	Version         string `json:"version"`
}

// InvokeParams invokes one leaf command.
type InvokeParams struct {
	Command   []string       `json:"command"`
	Arguments map[string]any `json:"arguments"`
	Context   InvokeContext  `json:"context"`
}

// InvokeContext propagates correlation and project identity. It carries no
// authority: the plugin acts as principal "plugin:<name>" regardless.
type InvokeContext struct {
	CorrelationID string `json:"correlation_id"`
	TraceParent   string `json:"traceparent"`
	ProjectID     string `json:"project_id,omitempty"`
	OutputFormat  string `json:"output_format"`
}

// InvokeResult is what a command returns. Output is validated against the
// command's output_schema. Proposals are candidates for the core to
// govern; returning them never admits anything. Cost entries report
// resource use; each must say whether it was measured, reported or
// estimated.
type InvokeResult struct {
	Output    json.RawMessage `json:"output,omitempty"`
	Text      string          `json:"text,omitempty"`
	Proposals []Proposal      `json:"proposals,omitempty"`
	Cost      []CostEntry     `json:"cost,omitempty"`
}

// Proposal is a candidate a plugin offers to the core. Kind is one of the
// proposal kinds below; Body is kind-specific and validated by the core.
type Proposal struct {
	Kind      string          `json:"kind"`
	Strategy  string          `json:"strategy"`
	Body      json.RawMessage `json:"body"`
	Rationale string          `json:"rationale,omitempty"`
}

// Proposal kinds.
const (
	ProposeGoal           = "goal"
	ProposeInvariant      = "invariant"
	ProposeWork           = "work"
	ProposeInterpretation = "interpretation"
	ProposeAmbiguity      = "ambiguity"
)

// CostEntry is one resource observation.
type CostEntry struct {
	Resource string  `json:"resource"` // e.g. "model.tokens.input", "api.usd"
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Basis    string  `json:"basis"` // measured | reported | estimated
	Source   string  `json:"source,omitempty"`
}

// Cost bases.
const (
	BasisMeasured  = "measured"
	BasisReported  = "reported"
	BasisEstimated = "estimated"
)

// TelemetryEvent is emitted by a plugin through MethodTelemetry. The host
// attaches plugin identity, project and correlation identifiers and filters
// attributes before recording.
type TelemetryEvent struct {
	Type       string         `json:"type"` // log | metric | span
	Name       string         `json:"name"`
	Severity   string         `json:"severity,omitempty"`
	Message    string         `json:"message,omitempty"`
	ErrorID    string         `json:"error_id,omitempty"`
	Value      float64        `json:"value,omitempty"`
	Unit       string         `json:"unit,omitempty"`
	DurationMS float64        `json:"duration_ms,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// HostIntentResult answers MethodHostIntent: the current intent revision.
type HostIntentResult struct {
	IntentID string `json:"intent_id"`
	Revision int    `json:"revision"`
	Content  string `json:"content"`
	Digest   string `json:"digest"`
}

// Conn reads and writes protocol messages on a stream pair. It is safe for
// concurrent writers.
type Conn struct {
	r  *bufio.Reader
	w  io.Writer
	mu sync.Mutex
}

// NewConn wraps a reader and writer.
func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{r: bufio.NewReaderSize(r, 64<<10), w: w}
}

// ErrMessageTooLarge is returned when a line exceeds MaxMessageBytes.
var ErrMessageTooLarge = errors.New("protocol message exceeds size limit")

// Read returns the next message.
func (c *Conn) Read() (*Message, error) {
	var line []byte
	for {
		chunk, isPrefix, err := c.r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > MaxMessageBytes {
			return nil, ErrMessageTooLarge
		}
		if !isPrefix {
			break
		}
	}
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, &Error{Code: CodeParseError, Message: "invalid JSON: " + err.Error()}
	}
	if m.JSONRPC != "2.0" {
		return nil, &Error{Code: CodeInvalidRequest, Message: `jsonrpc must be "2.0"`}
	}
	return &m, nil
}

// Write sends one message.
func (c *Conn) Write(m *Message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > MaxMessageBytes {
		return ErrMessageTooLarge
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// Request builds a request message.
func Request(id int64, method string, params any) (*Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	idRaw := json.RawMessage(fmt.Sprintf("%d", id))
	return &Message{ID: &idRaw, Method: method, Params: raw}, nil
}

// Notification builds a notification message.
func Notification(method string, params any) (*Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return &Message{Method: method, Params: raw}, nil
}

// Response builds a success response to req.
func Response(id *json.RawMessage, result any) (*Message, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return &Message{ID: id, Result: raw}, nil
}

// ErrorResponse builds an error response to req.
func ErrorResponse(id *json.RawMessage, e *Error) *Message {
	return &Message{ID: id, Error: e}
}

// NewError builds a structured error with a stable identifier.
func NewError(code int, id, message string) *Error {
	return &Error{Code: code, Message: message, Data: &ErrorData{ID: id}}
}

// HostContractParams asks for one work unit's contract.
type HostContractParams struct {
	Work string `json:"work"`
}

// HostContractResult is the work contract (gip.work-contract/v1) and the
// project root the executor works in.
type HostContractResult struct {
	Root     string          `json:"root"`
	Contract json.RawMessage `json:"contract"`
}

// Design returns the plan design the contract carries (SDK v0.7.0): the
// document the unit is built and reviewed to. It is nil when the plan has
// no design, or the gip predates it.
func (r *HostContractResult) Design() (*PlanDesign, error) {
	if len(r.Contract) == 0 {
		return nil, nil
	}
	var c struct {
		Design *PlanDesign `json:"design"`
	}
	if err := json.Unmarshal(r.Contract, &c); err != nil {
		return nil, err
	}
	return c.Design, nil
}

// Act kinds an executor may request.
const (
	ActStart     = "start"
	ActSubmit    = "submit"
	ActQualify   = "qualify"
	ActIntegrate = "integrate"
	// ActRework sends submitted, unintegrated work back to the executor
	// (for example to resolve a merge conflict with the integration
	// branch). Rationale carries the reason. The unit then needs fresh
	// evidence, review and qualification.
	ActRework = "rework"
	// ActRenew renews the lease the executor holds on a unit it started
	// (SDK v0.3.0). Lease names the lease being renewed; a lease that was
	// reclaimed or replaced is refused (LEASE_RECLAIMED, LEASE_SUPERSEDED).
	// A renewal by a principal whose work.start authority is gone is
	// refused outright.
	ActRenew = "renew"
	// ActReclaim returns a unit whose lease has expired to the frontier
	// (back to not started), recording the holder that lost it (SDK
	// v0.3.0). It needs a grant naming work.reclaim (wildcards do not cover
	// it for agents); a live lease is refused (LEASE_ACTIVE), and a claim
	// held by a human needs a human (HUMAN_REQUIRED). Rationale is the
	// reason: plain text, one line.
	ActReclaim = "reclaim"
	// ActReview decides an open review ceremony on Work (SDK v0.6.0).
	// Ceremony names the review ceremony (the one a submit opened, in
	// HostActResult.Ceremony), Outcome is ReviewApprove or ReviewReject,
	// and Rationale (required) says why. It needs the work.review plugin
	// permission (not work.execute) and, in the project, a work.review
	// grant for the acting principal. The executor of the unit can never
	// review it (CONFLICT_OF_INTEREST), and a plugin can never claim the
	// solo self-review waiver. A rejection returns the unit to its
	// executor for rework.
	ActReview = "review"
)

// Review outcomes (ActReview).
const (
	ReviewApprove = "approve"
	ReviewReject  = "reject"
)

// Lease states.
const (
	LeaseActive  = "active"
	LeaseExpired = "expired"
)

// Lease is the time-limited claim a work.start grants its executor
// (ADR-0015). Times are the gateway's ledger times; State is evaluated at
// the time the host answered.
type Lease struct {
	ID         string    `json:"id"`
	Holder     string    `json:"holder"`
	TTLSeconds int       `json:"ttl_seconds"`
	GrantedAt  time.Time `json:"granted_at"`
	RenewedAt  time.Time `json:"renewed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Renewals   int       `json:"renewals"`
	State      string    `json:"state"` // active | expired
}

// HostActParams asks the host to submit one work transition as principal
// As, which must be an agent principal the plugin was trusted to act as.
//
// Workspace is where the work was done: the project root (default) or a
// linked git worktree of the project's repository, so several units can
// be in flight at once. Checks run, code is observed and the qualified
// commit is taken there.
//
// RequestID makes the act idempotent: re-sending it (an executor resuming
// after a crash, or a pool reporting late) replays the recorded result.
//
// For submit, ResultPath names a gip.work-result/v1 document under
// <workspace>/.gip/results/<work>/ and RunChecks runs the defined checks.
type HostActParams struct {
	Kind       string `json:"kind"`
	Work       string `json:"work"`
	As         string `json:"as"`
	Workspace  string `json:"workspace,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	ResultPath string `json:"result_path,omitempty"`
	RunChecks  bool   `json:"run_checks,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
	// Lease names the lease the act is made under (submit, renew): the
	// id a start returned. A stale holder's act is refused even when it
	// acts as the same principal as the current holder. Empty: only the
	// principal is checked.
	Lease string `json:"lease,omitempty"`
	// LeaseSeconds asks for a lease time to live other than the
	// project's default (start, renew). The host clamps it to the
	// project's maximum.
	LeaseSeconds int `json:"lease_seconds,omitempty"`
	// RotateLease, on renew, replaces the named lease with a new one for
	// the same holder (its id is returned in the result). An executor
	// resuming a unit after a restart rotates, so an earlier instance
	// still holding the old id is refused (LEASE_SUPERSEDED). After a
	// rotation or a reclaim, every renew and submit must name the lease
	// (LEASE_REQUIRED).
	RotateLease bool `json:"rotate_lease,omitempty"`
	// Ceremony, on review, is the open review ceremony of Work being
	// decided (SDK v0.6.0).
	Ceremony string `json:"ceremony,omitempty"`
	// Outcome, on review, is ReviewApprove or ReviewReject.
	Outcome string `json:"outcome,omitempty"`
	// Evidence, on review, optionally names evidence records the
	// decision relies on.
	Evidence []string `json:"evidence,omitempty"`
}

// HostActResult is the gateway's disposition.
type HostActResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Ceremony    string   `json:"ceremony,omitempty"`
	EventSeq    int64    `json:"event_seq"`
	// Lease is the unit's lease after a start, renew or rework the act's
	// principal holds (SDK v0.3.0). A replayed start returns the lease it
	// granted only while that lease is still the unit's.
	Lease *Lease `json:"lease,omitempty"`
}

// HostProgressParams reports progress on a unit the executor holds, as
// principal As (one the plugin was trusted to act as). Phase is a short
// identifier (for example worktree, agent, checks, submit, integrate);
// Message is a short human-readable line; Percent is optional (0-100).
// Cost is what the unit has consumed so far, informational: it is shown
// with the progress and never added to cost totals (those come from the
// command's InvokeResult.Cost).
type HostProgressParams struct {
	Work    string      `json:"work"`
	As      string      `json:"as"`
	Lease   string      `json:"lease,omitempty"`
	Phase   string      `json:"phase"`
	Message string      `json:"message,omitempty"`
	Percent *float64    `json:"percent,omitempty"`
	Cost    []CostEntry `json:"cost,omitempty"`
}

// HostProgressResult says whether progress was recorded. A report for a
// unit the principal does not hold, or under a lease that is no longer
// the unit's, is not recorded: Code says why (NOT_EXECUTOR,
// NOT_IN_PROGRESS, LEASE_RECLAIMED, LEASE_SUPERSEDED), and an executor
// should stop working on the unit. Lease is the unit's current lease.
type HostProgressResult struct {
	Recorded bool   `json:"recorded"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Lease    *Lease `json:"lease,omitempty"`
}

// Progress phases the executor reports. Hosts accept any short
// identifier; these are the conventional ones.
const (
	PhaseClaimed   = "claimed"
	PhaseWorktree  = "worktree"
	PhaseAgent     = "agent"
	PhaseChecks    = "checks"
	PhaseSubmit    = "submit"
	PhaseQualify   = "qualify"
	PhaseIntegrate = "integrate"
	PhaseDone      = "done"
)

// HostExecutionParams asks for units in flight: one work unit, or every
// unit that is in progress when Work is empty. Limit bounds the progress
// events returned per unit (latest last; default 20).
type HostExecutionParams struct {
	Work  string `json:"work,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// ProgressEvent is one recorded progress report.
type ProgressEvent struct {
	At      time.Time   `json:"at"`
	Work    string      `json:"work"`
	Lease   string      `json:"lease,omitempty"`
	Phase   string      `json:"phase"`
	Message string      `json:"message,omitempty"`
	Percent *float64    `json:"percent,omitempty"`
	Cost    []CostEntry `json:"cost,omitempty"`
	Plugin  string      `json:"plugin,omitempty"`
}

// ExecutionStatus is one unit's execution, lease and recent progress.
type ExecutionStatus struct {
	Work      string          `json:"work"`
	Execution string          `json:"execution"`
	Executor  string          `json:"executor,omitempty"`
	Lease     *Lease          `json:"lease,omitempty"`
	Progress  []ProgressEvent `json:"progress"`
}

// HostExecutionResult answers MethodHostExecution. EvaluatedAt is the
// time lease states were evaluated at.
type HostExecutionResult struct {
	EvaluatedAt time.Time         `json:"evaluated_at"`
	Units       []ExecutionStatus `json:"units"`
}

// HostCheckParams asks the host to run a predicate's defined check for a
// work unit and record what it observed, as principal As.
type HostCheckParams struct {
	Work      string `json:"work"`
	Predicate string `json:"predicate"`
	As        string `json:"as"`
	Workspace string `json:"workspace,omitempty"` // as in HostActParams
	RequestID string `json:"request_id,omitempty"`
}

// HostCheckResult reports the observed run.
type HostCheckResult struct {
	Result      string `json:"result"` // PASS | FAIL | UNKNOWN
	ExitCode    int    `json:"exit_code"`
	Evidence    string `json:"evidence"`
	OutputTail  string `json:"output_tail"`
	Disposition string `json:"disposition"`
}

// HostConfigResult is the plugin's own configuration file
// (<config>/<short name>.yaml), parsed by the host. Found is false when
// the file does not exist.
type HostConfigResult struct {
	Found  bool            `json:"found"`
	Path   string          `json:"path"`
	Config json.RawMessage `json:"config,omitempty"`
}
