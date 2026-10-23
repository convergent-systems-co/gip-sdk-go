package gipplugin

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

const conflictsManifest = `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-t","version":"0.1.0","protocol_versions":[1],"executable":"t",
"permissions":["conflicts.resolve"],
"commands":[{"name":"t","summary":"s","subcommands":[{"name":"run","summary":"s","permissions":["conflicts.resolve"]}]}]}`

// conflictHost is a scripted host holding merge-conflict ceremonies with
// the gateway's dispositions for #108: an author, or an agent whose
// binding chain runs through one (chained), is refused
// (CONFLICT_OF_INTEREST) and the list says so for it; a stale revision is
// STALE_REVISION, a decided ceremony CONFLICT_NOT_OPEN, a candidate that
// cannot be adopted is listed with its refusal, records are withheld
// unless the ceremony is agent class with authors and the plugin holds
// state.read (stateRead), a reused request id with other parameters is
// IDEMPOTENCY_CONFLICT, and a decline raises one question per ceremony.
type conflictHost struct {
	open      map[string]*protocol.ConflictCeremony
	chained   map[string]bool
	stateRead bool
	questions int
	replays   map[string]resolveReplay
	calls     []string
	params    []json.RawMessage
}

type resolveReplay struct {
	p   protocol.HostConflictResolveParams
	res protocol.HostConflictResolveResult
}

func (h *conflictHost) refusal(c *protocol.ConflictCeremony, as string) *protocol.ConflictRefusal {
	switch {
	case c.Class == protocol.ConflictClassHuman:
		return &protocol.ConflictRefusal{Code: protocol.CodeHumanClassConflict, Reason: "human class"}
	case len(c.Authors) == 0:
		return &protocol.ConflictRefusal{Code: protocol.CodeAuthorsUnknown, Reason: "no authors"}
	case c.Question != "":
		return &protocol.ConflictRefusal{Code: protocol.CodeQuestionOpen, Reason: c.Question}
	case c.IsAuthor(as) || h.chained[as]:
		return &protocol.ConflictRefusal{Code: protocol.CodeConflictOfInterest, Reason: "author or chain"}
	}
	return nil
}

func newConflictHost() *conflictHost {
	return &conflictHost{
		open: map[string]*protocol.ConflictCeremony{
			"cer-7": {ID: "cer-7", Revision: 1, Object: protocol.ConflictObject{Kind: "work", ID: "w-3", Revision: 4},
				Class: protocol.ConflictClassAgent, Codes: []string{"DIVERGED"}, Authors: []string{"agent:builder"},
				Candidates: []protocol.ConflictCandidate{
					{ID: "a", Head: "h1", Revision: 4, Summary: "work w-3 submitted", Author: "agent:builder", Provisional: true, Record: json.RawMessage(`{"id":"w-3"}`)},
					{ID: "b", Head: "h2", Revision: 4, Summary: "work w-3 withdrawn", Author: "agent:planner", Record: json.RawMessage(`{"id":"w-3"}`)},
					{ID: "base", Revision: 3, Summary: "work w-3 ready", Author: "agent:planner", PresenceFlagged: true,
						Refusal: &protocol.ConflictRefusal{Code: protocol.CodePresenceEnrolled, Reason: "flagged"}, Record: json.RawMessage(`{"id":"w-3"}`)},
				}},
			"cer-9": {ID: "cer-9", Revision: 2, Object: protocol.ConflictObject{Kind: "intent", ID: "main", Revision: 5},
				Class: protocol.ConflictClassHuman, Authors: []string{"human:t"}, Question: "amb-12",
				Candidates: []protocol.ConflictCandidate{{ID: "a", Summary: "s", Record: json.RawMessage(`{"content":"secret"}`)}, {ID: "b", Summary: "s"}}},
		},
		chained:   map[string]bool{"agent:proxy": true},
		stateRead: true,
		replays:   map[string]resolveReplay{},
	}
}

func (h *conflictHost) answer(m *protocol.Message) *protocol.Message {
	h.calls = append(h.calls, m.Method)
	h.params = append(h.params, m.Params)
	var res any
	switch m.Method {
	case protocol.MethodHostConflictsList:
		var p protocol.HostConflictsListParams
		_ = json.Unmarshal(m.Params, &p)
		out := protocol.HostConflictsListResult{Conflicts: []protocol.ConflictCeremony{}}
		for _, id := range []string{"cer-7", "cer-9"} {
			if c := h.open[id]; c != nil && (p.Ceremony == "" || p.Ceremony == id) {
				x := *c
				x.Refusal = h.refusal(c, p.As)
				x.Candidates = nil
				for _, cand := range c.Candidates {
					cand.Adoptable = x.Refusal == nil && cand.Refusal == nil
					if x.Class != protocol.ConflictClassAgent || len(x.Authors) == 0 || !h.stateRead {
						cand.Record = nil
					}
					x.Candidates = append(x.Candidates, cand)
				}
				out.Conflicts = append(out.Conflicts, x)
			}
		}
		res = out
	case protocol.MethodHostConflictResolve:
		var p protocol.HostConflictResolveParams
		_ = json.Unmarshal(m.Params, &p)
		if r, ok := h.replays[p.RequestID]; ok {
			if r.p != p {
				return protocol.ErrorResponse(m.ID, protocol.NewError(protocol.CodeInvalidParams, protocol.CodeIdempotencyConflict, "request id reused"))
			}
			res = r.res
			break
		}
		c := h.open[p.Ceremony]
		out := protocol.HostConflictResolveResult{Disposition: protocol.DispositionRejected, Reasons: []string{"refused"}}
		switch {
		case c == nil:
			out.Code = protocol.CodeConflictNotOpen
		case c.Revision != p.Revision:
			out.Code = protocol.CodeStaleRevision
		case h.refusal(c, p.As) != nil:
			r := h.refusal(c, p.As)
			out.Code = r.Code
			if r.Code != protocol.CodeConflictOfInterest {
				out.Disposition = protocol.DispositionHumanRequired
			}
		case c.Candidate(p.Adopt) != nil && c.Candidate(p.Adopt).Refusal != nil:
			out.Code = c.Candidate(p.Adopt).Refusal.Code
		default:
			out = protocol.HostConflictResolveResult{Disposition: protocol.DispositionAccepted, Reasons: []string{}, EventSeq: 40}
			if p.Keep {
				out.Result = "KEPT a"
			} else {
				out.Result = "ADOPTED " + p.Adopt
			}
			delete(h.open, p.Ceremony)
			h.replays[p.RequestID] = resolveReplay{p, out}
		}
		res = out
	case protocol.MethodHostConflictDecline:
		var p protocol.HostConflictDeclineParams
		_ = json.Unmarshal(m.Params, &p)
		c := h.open[p.Ceremony]
		switch {
		case c == nil:
			res = protocol.HostConflictDeclineResult{Disposition: protocol.DispositionRejected, Code: protocol.CodeConflictNotOpen, Reasons: []string{"not open"}}
		case c.Question != "":
			res = protocol.HostConflictDeclineResult{Disposition: protocol.DispositionAccepted, Reasons: []string{}, Question: c.Question}
		default:
			h.questions++
			c.Question = "amb-20"
			c.Revision++
			res = protocol.HostConflictDeclineResult{Disposition: protocol.DispositionAccepted, Reasons: []string{}, Question: c.Question, Raised: true, EventSeq: 41}
		}
	default:
		return protocol.ErrorResponse(m.ID, protocol.NewError(protocol.CodeMethodNotFound, "host.method_not_found", m.Method))
	}
	resp, _ := protocol.Response(m.ID, res)
	return resp
}

// runConflicts serves one invocation of handler with the given grants
// against host.
func runConflicts(t *testing.T, granted []string, host *conflictHost, handler Handler) *protocol.Message {
	t.Helper()
	hostIn, pluginOut := io.Pipe()
	pluginIn, hostOut := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- ServeIO(context.Background(), []byte(conflictsManifest), Handlers{"t run": handler}, pluginIn, pluginOut)
	}()
	conn := protocol.NewConn(hostIn, hostOut)
	send := func(m *protocol.Message) {
		if err := conn.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	init, _ := protocol.Request(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{1}, GrantedPermissions: granted})
	send(init)
	if m, err := conn.Read(); err != nil || m.Error != nil {
		t.Fatalf("initialize: %+v %v", m, err)
	}
	inv, _ := protocol.Request(2, protocol.MethodInvoke, protocol.InvokeParams{Command: []string{"t", "run"}, Arguments: map[string]any{}})
	send(inv)
	for {
		m, err := conn.Read()
		if err != nil {
			t.Fatal(err)
		}
		if m.Method == "" {
			sd, _ := protocol.Notification(protocol.MethodShutdown, struct{}{})
			send(sd)
			if code := <-done; code != 0 {
				t.Fatalf("exit %d", code)
			}
			return m
		}
		if m.IsRequest() {
			send(host.answer(m))
		}
	}
}

// TestConflicts drives list, resolve and decline through ServeIO: the
// list carries gip's verdict for the listing principal (including a chain
// conflict Authors cannot show) and each candidate's adoptability; records
// are withheld for a human-class ceremony; an independent agent adopts a
// candidate (replayed by an identical request, IDEMPOTENCY_CONFLICT for a
// changed one); an author, a stale revision, a decided ceremony and an
// unadoptable candidate are refused; a human-class conflict needs a person
// (#108, ADR-0025 §3).
func TestConflicts(t *testing.T) {
	host := newConflictHost()
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		l, err := r.ConflictsList(ctx, "agent:reviewer", "")
		if err != nil || len(l.Conflicts) != 2 {
			t.Fatalf("list: %+v %v", l, err)
		}
		c := l.Conflicts[0]
		if !c.Eligible() || !c.AgentResolvable() || c.QuestionOpen() || !c.IsAuthor("agent:builder") || !c.Candidate("a").Provisional {
			t.Errorf("cer-7: %+v", c)
		}
		if b, base := c.Candidate("b"), c.Candidate("base"); !b.Adoptable || b.Record == nil || base.Adoptable || base.Refusal.Code != protocol.CodePresenceEnrolled {
			t.Errorf("candidates: %+v %+v", b, base)
		}
		if h := l.Conflicts[1]; h.Eligible() || h.Refusal.Code != protocol.CodeHumanClassConflict || !h.QuestionOpen() || h.Candidate("a").Record != nil || h.Candidate("a").Adoptable {
			t.Errorf("cer-9 is human-class with a question open and no records: %+v", h)
		}
		// agent:proxy is not an author, but its chain runs through one.
		px, err := r.ConflictsList(ctx, "agent:proxy", "cer-7")
		if err != nil || px.Conflicts[0].IsAuthor("agent:proxy") || px.Conflicts[0].Eligible() || px.Conflicts[0].Refusal.Code != protocol.CodeConflictOfInterest {
			t.Errorf("chain conflict: %+v %v", px, err)
		}
		if px.Conflicts[0].Candidate("b").Adoptable {
			t.Error("no candidate is adoptable for a refused principal")
		}
		one, err := r.ConflictsList(ctx, "agent:reviewer", "cer-9")
		if err != nil || len(one.Conflicts) != 1 || one.Conflicts[0].ID != "cer-9" {
			t.Errorf("list one: %+v %v", one, err)
		}
		res, err := r.ConflictResolve(ctx, protocol.HostConflictResolveParams{As: "agent:builder", Ceremony: "cer-7", Revision: 1, Adopt: "b", Rationale: "mine", RequestID: "r-0"})
		if err != nil || res.Disposition != protocol.DispositionRejected || res.Code != protocol.CodeConflictOfInterest {
			t.Errorf("an author is refused: %+v %v", res, err)
		}
		res, err = r.ConflictResolve(ctx, protocol.HostConflictResolveParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 9, Adopt: "b", Rationale: "r", RequestID: "r-1"})
		if err != nil || res.Code != protocol.CodeStaleRevision {
			t.Errorf("stale: %+v %v", res, err)
		}
		res, err = r.ConflictResolve(ctx, protocol.HostConflictResolveParams{As: "agent:reviewer", Ceremony: "cer-9", Revision: 2, Keep: true, Rationale: "r", RequestID: "r-2"})
		if err != nil || res.Disposition != protocol.DispositionHumanRequired || res.Code != protocol.CodeHumanClassConflict {
			t.Errorf("human class: %+v %v", res, err)
		}
		res, err = r.ConflictResolve(ctx, protocol.HostConflictResolveParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 1, Adopt: "base", Rationale: "r", RequestID: "r-4"})
		if err != nil || res.Code != protocol.CodePresenceEnrolled {
			t.Errorf("unadoptable candidate: %+v %v", res, err)
		}
		p := protocol.HostConflictResolveParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 1, Adopt: "b", Rationale: "the withdrawal\r\nis later", RequestID: "r-3"}
		res, err = r.ConflictResolve(ctx, p)
		if err != nil || res.Disposition != protocol.DispositionAccepted || res.Result != "ADOPTED b" {
			t.Errorf("resolve: %+v %v", res, err)
		}
		if again, err := r.ConflictResolve(ctx, p); err != nil || again.Disposition != res.Disposition || again.Result != res.Result || again.EventSeq != res.EventSeq {
			t.Errorf("an identical request replays the recorded result: %+v %v", again, err)
		}
		changed := p
		changed.Adopt = "a"
		if _, err := r.ConflictResolve(ctx, changed); !protocol.IsIdempotencyConflict(err) {
			t.Errorf("a reused request id with other parameters: %v", err)
		}
		d, err := r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 1, Reason: "gone"})
		if err != nil || d.Code != protocol.CodeConflictNotOpen {
			t.Errorf("decline a decided ceremony: %+v %v", d, err)
		}
		d, err = r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "agent:reviewer", Ceremony: "cer-9", Revision: 2, Reason: "a person must pick"})
		if err != nil || d.Disposition != protocol.DispositionAccepted || d.Question != "amb-12" || d.Raised {
			t.Errorf("decline with a question open returns it: %+v %v", d, err)
		}
		return &protocol.InvokeResult{Text: "ok"}, nil
	}
	if m := runConflicts(t, []string{protocol.PermConflictsResolve}, host, handler); m.Error != nil {
		t.Fatal(m.Error)
	}
	if host.questions != 0 {
		t.Fatalf("no question should be raised: %d", host.questions)
	}
	// The rationale reached the host with LF only.
	var sent protocol.HostConflictResolveParams
	for i, m := range host.calls {
		if m == protocol.MethodHostConflictResolve {
			_ = json.Unmarshal(host.params[i], &sent)
		}
	}
	if sent.Rationale != "the withdrawal\nis later" {
		t.Fatalf("rationale %q", sent.Rationale)
	}
}

// TestConflictDeclineRaisesOneQuestion: a decline of a conflict no
// question names raises one; a second decline returns the same question.
func TestConflictDeclineRaisesOneQuestion(t *testing.T) {
	host := newConflictHost()
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		d, err := r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 1, Reason: "both versions are plausible", RequestID: "d-1"})
		if err != nil || !d.Raised || d.Question != "amb-20" {
			t.Errorf("decline: %+v %v", d, err)
		}
		d, err = r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "agent:other", Ceremony: "cer-7", Revision: 2, Reason: "same"})
		if err != nil || d.Raised || d.Question != "amb-20" {
			t.Errorf("second decline: %+v %v", d, err)
		}
		l, err := r.ConflictsList(ctx, "agent:reviewer", "cer-7")
		if err != nil || len(l.Conflicts) != 1 || !l.Conflicts[0].QuestionOpen() || l.Conflicts[0].AgentResolvable() || l.Conflicts[0].Refusal.Code != protocol.CodeQuestionOpen {
			t.Errorf("after decline: %+v %v", l, err)
		}
		return nil, nil
	}
	runConflicts(t, []string{protocol.PermConflictsResolve}, host, handler)
	if host.questions != 1 {
		t.Fatalf("one question per ceremony, got %d", host.questions)
	}
}

// TestConflictsAreGatedAndValidated: without conflicts.resolve, or with a
// request of the wrong shape, nothing is sent to the host.
func TestConflictsAreGatedAndValidated(t *testing.T) {
	host := newConflictHost()
	ok := protocol.HostConflictResolveParams{As: "agent:reviewer", Ceremony: "cer-7", Revision: 1, Adopt: "a", Rationale: "r", RequestID: "r-1"}
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		if _, err := r.ConflictsList(ctx, "agent:reviewer", ""); protocol.ErrorID(err) != protocol.ErrorIDPermissionDenied {
			t.Errorf("list without conflicts.resolve: %v", err)
		}
		if _, err := r.ConflictResolve(ctx, ok); protocol.ErrorID(err) != protocol.ErrorIDPermissionDenied {
			t.Errorf("resolve without conflicts.resolve: %v", err)
		}
		if _, err := r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "a", Ceremony: "c", Revision: 1, Reason: "r"}); protocol.ErrorID(err) != protocol.ErrorIDPermissionDenied {
			t.Errorf("decline without conflicts.resolve: %v", err)
		}
		return nil, nil
	}
	runConflicts(t, []string{protocol.PermWorkReview}, host, handler)
	handler = func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		bad := []protocol.HostConflictResolveParams{}
		for _, f := range []func(p *protocol.HostConflictResolveParams){
			func(p *protocol.HostConflictResolveParams) { p.Keep = true },
			func(p *protocol.HostConflictResolveParams) { p.Adopt = "" },
			func(p *protocol.HostConflictResolveParams) { p.Rationale = " \n" },
			func(p *protocol.HostConflictResolveParams) {
				p.Rationale = strings.Repeat("x", protocol.MaxConflictRationaleBytes+1)
			},
			func(p *protocol.HostConflictResolveParams) { p.Rationale = "bell\a" },
			func(p *protocol.HostConflictResolveParams) { p.RequestID = "" },
			func(p *protocol.HostConflictResolveParams) { p.Revision = 0 },
			func(p *protocol.HostConflictResolveParams) { p.Ceremony = "" },
			func(p *protocol.HostConflictResolveParams) { p.As = "" },
			func(p *protocol.HostConflictResolveParams) { p.Ceremony = "cer 7" },
		} {
			p := ok
			f(&p)
			bad = append(bad, p)
		}
		for _, p := range bad {
			if _, err := r.ConflictResolve(ctx, p); protocol.ErrorID(err) != protocol.CodeConflictRequestInvalid {
				t.Errorf("resolve %+v: %v", p, err)
			}
		}
		if _, err := r.ConflictDecline(ctx, protocol.HostConflictDeclineParams{As: "a", Ceremony: "c", Revision: 1}); protocol.ErrorID(err) != protocol.CodeConflictRequestInvalid {
			t.Errorf("decline without a reason: %v", err)
		}
		if _, err := r.ConflictsList(ctx, "agent:reviewer", strings.Repeat("c", protocol.MaxConflictIDBytes+1)); protocol.ErrorID(err) != protocol.CodeConflictRequestInvalid {
			t.Errorf("list with a long id: %v", err)
		}
		if _, err := r.ConflictsList(ctx, "", ""); protocol.ErrorID(err) != protocol.CodeConflictRequestInvalid {
			t.Errorf("list without as: %v", err)
		}
		return nil, nil
	}
	runConflicts(t, []string{protocol.PermConflictsResolve}, host, handler)
	if len(host.calls) != 0 {
		t.Fatalf("refused requests reached the host: %q", host.calls)
	}
}

// TestConflictRecordsNeedStateRead: without state.read the host withholds
// every record; summaries stay.
func TestConflictRecordsNeedStateRead(t *testing.T) {
	host := newConflictHost()
	host.stateRead = false
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		l, err := r.ConflictsList(ctx, "agent:reviewer", "cer-7")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range l.Conflicts[0].Candidates {
			if c.Record != nil || c.Summary == "" {
				t.Errorf("candidate %+v", c)
			}
		}
		return nil, nil
	}
	runConflicts(t, []string{protocol.PermConflictsResolve}, host, handler)
}
