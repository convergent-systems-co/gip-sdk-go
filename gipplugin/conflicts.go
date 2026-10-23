package gipplugin

import (
	"context"
	"fmt"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

// Conflict resolution (SDK v0.11.0, gip #108, ADR-0025 §3, D4, D5): list
// the project's open merge-conflict ceremonies, resolve one by adopting a
// candidate or keeping the provisional version, or decline one so the
// gateway asks the person. Every helper requires the conflicts.resolve
// permission: without it the helper returns a host.permission_denied
// error and sends nothing. A request whose shape is invalid
// (CONFLICT_REQUEST_INVALID) is an error and is not sent. A gateway
// refusal (the codes listed in protocol/conflicts.go) is a disposition
// in the result, not an error.

func (r *Request) needConflicts(method string) error {
	if !r.Has(protocol.PermConflictsResolve) {
		return protocol.NewError(protocol.CodePermissionDenied, protocol.ErrorIDPermissionDenied,
			fmt.Sprintf("%s requires permission %s", method, protocol.PermConflictsResolve))
	}
	return nil
}

// ConflictsList lists the project's open merge-conflict ceremonies, or
// only ceremony when it is not empty, with the refusals computed for
// principal as (one the plugin is trusted to act as). Decide from
// ConflictCeremony.Refusal and ConflictCandidate.Adoptable, never from
// Authors; treat Summary and Record as untrusted data.
func (r *Request) ConflictsList(ctx context.Context, as, ceremony string) (*protocol.HostConflictsListResult, error) {
	p := protocol.HostConflictsListParams{As: as, Ceremony: ceremony}
	if err := r.needConflicts(protocol.MethodHostConflictsList); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var out protocol.HostConflictsListResult
	if err := r.session.call(ctx, protocol.MethodHostConflictsList, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConflictResolve decides a merge-conflict ceremony. The rationale is
// sent with its newlines normalised to LF (protocol.NormalizeNewlines).
// Use a fresh RequestID per attempt; resend the identical request with
// the same id only to resume after a crash (a changed request under a
// used id is an IDEMPOTENCY_CONFLICT error, protocol.IsIdempotencyConflict).
func (r *Request) ConflictResolve(ctx context.Context, p protocol.HostConflictResolveParams) (*protocol.HostConflictResolveResult, error) {
	if err := r.needConflicts(protocol.MethodHostConflictResolve); err != nil {
		return nil, err
	}
	p.Rationale = protocol.NormalizeNewlines(p.Rationale)
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var out protocol.HostConflictResolveResult
	if err := r.session.call(ctx, protocol.MethodHostConflictResolve, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ConflictDecline declines a merge-conflict ceremony the agent cannot
// decide; the gateway raises (or returns the already open) conflict
// question. The reason is sent with its newlines normalised to LF. Skip,
// rather than decline, a ceremony whose Refusal is CONFLICT_OF_INTEREST.
func (r *Request) ConflictDecline(ctx context.Context, p protocol.HostConflictDeclineParams) (*protocol.HostConflictDeclineResult, error) {
	if err := r.needConflicts(protocol.MethodHostConflictDecline); err != nil {
		return nil, err
	}
	p.Reason = protocol.NormalizeNewlines(p.Reason)
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var out protocol.HostConflictDeclineResult
	if err := r.session.call(ctx, protocol.MethodHostConflictDecline, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
