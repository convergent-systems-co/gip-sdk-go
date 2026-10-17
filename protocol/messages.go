package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
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

// Act kinds an executor may request.
const (
	ActStart     = "start"
	ActSubmit    = "submit"
	ActQualify   = "qualify"
	ActIntegrate = "integrate"
)

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
}

// HostActResult is the gateway's disposition.
type HostActResult struct {
	Disposition string   `json:"disposition"`
	Code        string   `json:"code,omitempty"`
	Reasons     []string `json:"reasons"`
	Ceremony    string   `json:"ceremony,omitempty"`
	EventSeq    int64    `json:"event_seq"`
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
