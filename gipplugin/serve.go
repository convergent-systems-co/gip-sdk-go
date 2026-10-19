// Package gipplugin is the Go SDK for writing a gip plugin. A plugin is an
// ordinary executable that ships a gip-plugin.json manifest and calls Serve.
//
//	func main() {
//		os.Exit(gipplugin.Serve(manifestJSON, gipplugin.Handlers{
//			"discover goals": discoverGoals,
//		}))
//	}
//
// Serve speaks the protocol defined in package protocol on stdin/stdout.
// Plugins must not write anything else to stdout; use stderr or the
// telemetry methods on Request.
package gipplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

// Handler implements one leaf command.
type Handler func(ctx context.Context, req *Request) (*protocol.InvokeResult, error)

// Handlers maps a space-separated command path ("discover goals") to its
// implementation.
type Handlers map[string]Handler

// Request is one command invocation.
type Request struct {
	Command   []string
	Arguments map[string]any
	Context   protocol.InvokeContext
	Granted   []string

	session *session
}

// String returns a string argument or "".
func (r *Request) String(name string) string { s, _ := r.Arguments[name].(string); return s }

// Bool returns a boolean argument or false.
func (r *Request) Bool(name string) bool { b, _ := r.Arguments[name].(bool); return b }

// Has reports whether permission p was granted by the host's trust policy.
func (r *Request) Has(p string) bool {
	for _, g := range r.Granted {
		if g == p {
			return true
		}
	}
	return false
}

// Log emits a structured log event through the host's telemetry. The host
// adds plugin, project and correlation identity.
func (r *Request) Log(severity, errorID, message string, attrs map[string]any) {
	r.session.notify(protocol.TelemetryEvent{Type: "log", Name: "plugin.log", Severity: severity, ErrorID: errorID, Message: message, Attributes: attrs})
}

// Metric emits a metric observation.
func (r *Request) Metric(name string, value float64, unit string, attrs map[string]any) {
	r.session.notify(protocol.TelemetryEvent{Type: "metric", Name: name, Value: value, Unit: unit, Attributes: attrs})
}

// Span records a completed span measured by the plugin.
func (r *Request) Span(name string, d time.Duration, attrs map[string]any) {
	r.session.notify(protocol.TelemetryEvent{Type: "span", Name: name, DurationMS: float64(d.Microseconds()) / 1000, Attributes: attrs})
}

// Intent asks the host for the current intent revision. Requires the
// intent.read permission.
func (r *Request) Intent(ctx context.Context) (*protocol.HostIntentResult, error) {
	var out protocol.HostIntentResult
	if err := r.session.call(ctx, protocol.MethodHostIntent, struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// State asks the host for a read-only snapshot of governed state. Requires
// the state.read permission. The shape is documented in
// docs/spec/plugin-protocol.md.
func (r *Request) State(ctx context.Context, out any) error {
	return r.session.call(ctx, protocol.MethodHostState, struct{}{}, out)
}

// Frontier asks the host for the current frontier (gip frontier discover
// --output json) and decodes it into out. Requires work.read.
func (r *Request) Frontier(ctx context.Context, out any) error {
	var raw json.RawMessage
	if err := r.session.call(ctx, protocol.MethodHostFrontier, struct{}{}, &raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Contract asks the host for a work unit's contract (gip.work-contract/v1)
// and decodes it into out; it returns the project root. Requires
// work.read.
func (r *Request) Contract(ctx context.Context, work string, out any) (string, error) {
	var res protocol.HostContractResult
	if err := r.session.call(ctx, protocol.MethodHostContract, protocol.HostContractParams{Work: work}, &res); err != nil {
		return "", err
	}
	return res.Root, json.Unmarshal(res.Contract, out)
}

// Act asks the host to submit a work transition as a trusted agent
// principal. Requires work.execute; the review, goal.qualify and
// goal.accept kinds require work.review instead. A refusal is a result,
// not an error.
func (r *Request) Act(ctx context.Context, p protocol.HostActParams) (*protocol.HostActResult, error) {
	var out protocol.HostActResult
	if err := r.session.call(ctx, protocol.MethodHostAct, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Check asks the host to run a predicate's defined check and record what
// it observed. Requires work.execute (or, for a goal subject, work.review).
func (r *Request) Check(ctx context.Context, p protocol.HostCheckParams) (*protocol.HostCheckResult, error) {
	var out protocol.HostCheckResult
	if err := r.session.call(ctx, protocol.MethodHostCheck, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Progress reports progress on a unit the executor holds (SDK v0.3.0).
// Requires work.execute. A report the host does not record (the unit is
// not held under this lease) is a result with Recorded false, not an
// error.
func (r *Request) Progress(ctx context.Context, p protocol.HostProgressParams) (*protocol.HostProgressResult, error) {
	var out protocol.HostProgressResult
	if err := r.session.call(ctx, protocol.MethodHostProgress, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Execution reads the lease and latest progress of one unit, or of every
// unit in progress when work is empty (SDK v0.3.0). Requires work.read.
func (r *Request) Execution(ctx context.Context, work string) (*protocol.HostExecutionResult, error) {
	var out protocol.HostExecutionResult
	if err := r.session.call(ctx, protocol.MethodHostExecution, protocol.HostExecutionParams{Work: work}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Plan reads gip's plan document (SDK v0.4.0, ADR-0021). With the digest
// of the document the caller holds, an unchanged plan comes back as
// Unchanged without the document. Requires work.read.
func (r *Request) Plan(ctx context.Context, ifDigest string) (*protocol.HostPlanResult, error) {
	var out protocol.HostPlanResult
	if err := r.session.call(ctx, protocol.MethodHostPlan, protocol.HostPlanParams{IfDigest: ifDigest}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlanReport reads the plan document like Plan and reports the
// executor's idle slots with it (HostPlanParams.IdleSlots): a positive
// count reports idle slots, 0 withdraws the report. Requires work.read.
func (r *Request) PlanReport(ctx context.Context, ifDigest string, idleSlots int) (*protocol.HostPlanResult, error) {
	var out protocol.HostPlanResult
	if idleSlots < 0 {
		idleSlots = 0
	}
	if err := r.session.call(ctx, protocol.MethodHostPlan, protocol.HostPlanParams{IfDigest: ifDigest, IdleSlots: &idleSlots}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Finding records discovered work against a plan node (SDK v0.4.0). A
// refusal is a disposition, not an error. Requires work.execute.
func (r *Request) Finding(ctx context.Context, p protocol.HostFindingParams) (*protocol.HostFindingResult, error) {
	var out protocol.HostFindingResult
	if err := r.session.call(ctx, protocol.MethodHostFinding, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PlanPass submits one planning pass (SDK v0.4.0). A refusal is a
// disposition, not an error. Requires plan.write.
func (r *Request) PlanPass(ctx context.Context, p protocol.HostPlanPassParams) (*protocol.HostPlanPassResult, error) {
	var out protocol.HostPlanPassResult
	if err := r.session.call(ctx, protocol.MethodHostPlanPass, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IntentPropose records a proposed intent revision (SDK v0.9.0, gip
// ADR-0024 §6) as a trusted agent principal. It changes nothing until a
// person adopts it. A refusal is a disposition, not an error. Requires
// plan.write.
func (r *Request) IntentPropose(ctx context.Context, p protocol.HostIntentProposeParams) (*protocol.HostIntentProposeResult, error) {
	var out protocol.HostIntentProposeResult
	if err := r.session.call(ctx, protocol.MethodHostIntentPropose, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeliveryPrepare has gip prepare the delivery for the current intent
// revisions (SDK v0.9.0, gip ADR-0024 §7) as a trusted reviewer
// principal. A refusal is a disposition, not an error. Requires
// work.review.
func (r *Request) DeliveryPrepare(ctx context.Context, p protocol.HostDeliveryPrepareParams) (*protocol.HostDeliveryPrepareResult, error) {
	var out protocol.HostDeliveryPrepareResult
	if err := r.session.call(ctx, protocol.MethodHostDeliveryPrepare, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Config asks the host for this plugin's configuration file, parsed, and
// decodes it into out. It reports false when there is none. Requires
// config.read.
func (r *Request) Config(ctx context.Context, out any) (bool, error) {
	var res protocol.HostConfigResult
	if err := r.session.call(ctx, protocol.MethodHostConfig, struct{}{}, &res); err != nil {
		return false, err
	}
	if !res.Found {
		return false, nil
	}
	return true, json.Unmarshal(res.Config, out)
}

// Fail returns a structured command failure with a stable error id.
func Fail(errorID, format string, a ...any) error {
	return protocol.NewError(protocol.CodeCommandFailed, errorID, fmt.Sprintf(format, a...))
}

// Serve runs the protocol loop on stdin/stdout and returns an exit code.
func Serve(manifest []byte, handlers Handlers) int {
	return ServeIO(context.Background(), manifest, handlers, os.Stdin, os.Stdout)
}

// ServeIO runs the protocol loop on the given streams.
func ServeIO(ctx context.Context, manifest []byte, handlers Handlers, in io.Reader, out io.Writer) int {
	m, err := protocol.ParseManifest(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gipplugin: invalid embedded manifest:", err)
		return 2
	}
	for _, leaf := range m.Leaves() {
		if handlers[strings.Join(leaf, " ")] == nil {
			fmt.Fprintf(os.Stderr, "gipplugin: no handler for command %q\n", strings.Join(leaf, " "))
			return 2
		}
	}
	s := &session{conn: protocol.NewConn(in, out), pending: map[string]chan *protocol.Message{}}
	return s.run(ctx, m, handlers)
}

type session struct {
	conn    *protocol.Conn
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[string]chan *protocol.Message
	granted []string
	inits   bool
	wg      sync.WaitGroup
}

func (s *session) run(ctx context.Context, m *protocol.Manifest, handlers Handlers) int {
	for {
		msg, err := s.conn.Read()
		if err != nil {
			s.wg.Wait()
			if errors.Is(err, io.EOF) {
				return 0
			}
			var pe *protocol.Error
			if errors.As(err, &pe) {
				_ = s.conn.Write(protocol.ErrorResponse(nil, pe))
				continue
			}
			fmt.Fprintln(os.Stderr, "gipplugin: read:", err)
			return 1
		}
		switch {
		case msg.Method == "" && msg.ID != nil: // response to a host call
			s.mu.Lock()
			ch := s.pending[string(*msg.ID)]
			delete(s.pending, string(*msg.ID))
			s.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		case msg.Method == protocol.MethodShutdown:
			s.wg.Wait()
			return 0
		case msg.Method == protocol.MethodInitialize && msg.IsRequest():
			var p protocol.InitializeParams
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				_ = s.conn.Write(protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeInvalidParams, "protocol.invalid_params", err.Error())))
				continue
			}
			chosen := 0
			for _, v := range p.ProtocolVersions {
				if m.SupportsVersion(v) && v > chosen {
					chosen = v
				}
			}
			if chosen == 0 {
				_ = s.conn.Write(protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeIncompatible, "protocol.incompatible", "no common protocol version")))
				continue
			}
			s.granted = p.GrantedPermissions
			s.inits = true
			resp, _ := protocol.Response(msg.ID, protocol.InitializeResult{ProtocolVersion: chosen, Name: m.Name, Version: m.Version})
			_ = s.conn.Write(resp)
		case msg.Method == protocol.MethodInvoke && msg.IsRequest():
			if !s.inits {
				_ = s.conn.Write(protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeInvalidRequest, "protocol.not_initialized", "initialize first")))
				continue
			}
			s.wg.Add(1)
			go func(msg *protocol.Message) {
				defer s.wg.Done()
				_ = s.conn.Write(s.invoke(ctx, m, handlers, msg))
			}(msg)
		case msg.IsRequest():
			_ = s.conn.Write(protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeMethodNotFound, "protocol.method_not_found", "unknown method "+msg.Method)))
		default:
			// Unknown notifications are ignored, per JSON-RPC.
		}
	}
}

func (s *session) invoke(ctx context.Context, m *protocol.Manifest, handlers Handlers, msg *protocol.Message) *protocol.Message {
	var p protocol.InvokeParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeInvalidParams, "protocol.invalid_params", err.Error()))
	}
	cmd := m.Find(p.Command)
	h := handlers[strings.Join(p.Command, " ")]
	if cmd == nil || len(cmd.Subcommands) > 0 || h == nil {
		return protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeMethodNotFound, "protocol.unknown_command", "unknown command "+strings.Join(p.Command, " ")))
	}
	if err := protocol.ValidateArgs(cmd.Arguments, p.Arguments); err != nil {
		return protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeInvalidParams, "protocol.invalid_arguments", err.Error()))
	}
	req := &Request{Command: p.Command, Arguments: p.Arguments, Context: p.Context, Granted: s.granted, session: s}
	res, err := safeCall(ctx, h, req)
	if err != nil {
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			pe = protocol.NewError(protocol.CodeCommandFailed, "plugin.command_failed", err.Error())
		}
		return protocol.ErrorResponse(msg.ID, pe)
	}
	if res == nil {
		res = &protocol.InvokeResult{}
	}
	resp, err := protocol.Response(msg.ID, res)
	if err != nil {
		return protocol.ErrorResponse(msg.ID, protocol.NewError(protocol.CodeInternal, "plugin.encode_failed", err.Error()))
	}
	return resp
}

func safeCall(ctx context.Context, h Handler, req *Request) (res *protocol.InvokeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = protocol.NewError(protocol.CodeInternal, "plugin.panic", fmt.Sprint(r))
		}
	}()
	return h(ctx, req)
}

func (s *session) notify(ev protocol.TelemetryEvent) {
	msg, err := protocol.Notification(protocol.MethodTelemetry, ev)
	if err == nil {
		_ = s.conn.Write(msg)
	}
}

func (s *session) call(ctx context.Context, method string, params, out any) error {
	id := s.nextID.Add(1)
	msg, err := protocol.Request(id, method, params)
	if err != nil {
		return err
	}
	ch := make(chan *protocol.Message, 1)
	s.mu.Lock()
	s.pending[string(*msg.ID)] = ch
	s.mu.Unlock()
	if err := s.conn.Write(msg); err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		return json.Unmarshal(resp.Result, out)
	case <-ctx.Done():
		return ctx.Err()
	}
}
