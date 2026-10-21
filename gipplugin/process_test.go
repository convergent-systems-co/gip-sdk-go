package gipplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestCheckChildRefusesTheChannel(t *testing.T) {
	ctx := context.Background()
	if err := CheckChild(Command(ctx, "true")); err != nil {
		t.Fatalf("Command must pass its own check: %v", err)
	}
	for name, mutate := range map[string]func(*exec.Cmd){
		"stdin":       func(c *exec.Cmd) { c.Stdin = os.Stdin },
		"stdout":      func(c *exec.Cmd) { c.Stdout = os.Stdout },
		"stderr":      func(c *exec.Cmd) { c.Stderr = os.Stdout },
		"extra stdin": func(c *exec.Cmd) { c.ExtraFiles = []*os.File{nil, os.Stdin} },
	} {
		c := Command(ctx, "true")
		mutate(c)
		if err := CheckChild(c); !errors.Is(err, ErrChildInheritsChannel) {
			t.Errorf("%s: %v", name, err)
		}
	}
	plain := exec.Command("true")
	plain.Stderr = os.Stderr
	if err := CheckChild(plain); err != nil {
		t.Fatalf("stderr is not the channel: %v", err)
	}
}
