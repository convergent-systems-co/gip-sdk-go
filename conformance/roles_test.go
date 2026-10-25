package conformance

import (
	"strings"
	"testing"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

// CNF-009 checks role command shapes statically (SDK v0.12.2).
func TestRoleShapes(t *testing.T) {
	good := &protocol.Manifest{
		Capabilities: []string{protocol.CapReviewIntent},
		Commands: []protocol.Command{{Name: "planner", Subcommands: []protocol.Command{{
			Name: "review", Role: protocol.RoleIntentReview,
			Arguments: []protocol.Argument{{Name: protocol.IntentReviewRequestArg, Type: protocol.ArgString, Required: true}},
		}}}},
	}
	if !declaresRoles(good) {
		t.Fatal("declares a role")
	}
	if err := roleShapes(good); err != nil {
		t.Fatal(err)
	}
	noArg := *good
	noArg.Commands = []protocol.Command{{Name: "review", Role: protocol.RoleIntentReview}}
	if err := roleShapes(&noArg); err == nil || !strings.Contains(err.Error(), "request") {
		t.Fatalf("missing request argument: %v", err)
	}
	noCmd := &protocol.Manifest{Capabilities: []string{protocol.CapReviewIntent}, Commands: []protocol.Command{{Name: "x"}}}
	if err := roleShapes(noCmd); err == nil {
		t.Fatal("review.intent without a role command")
	}
	noCap := &protocol.Manifest{Commands: noArg.Commands}
	if err := roleShapes(noCap); err == nil {
		t.Fatal("role without its capability")
	}
	if declaresRoles(&protocol.Manifest{Commands: []protocol.Command{{Name: "x"}}}) {
		t.Fatal("a plugin without roles skips CNF-009")
	}
}
