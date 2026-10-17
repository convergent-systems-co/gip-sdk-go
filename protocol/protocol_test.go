package protocol

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	decl := []Argument{
		{Name: "id", Type: ArgString, Positional: true, Required: true},
		{Name: "count", Type: ArgInteger},
		{Name: "tag", Type: ArgString, Repeated: true},
		{Name: "dry", Type: ArgBoolean, Short: "n"},
		{Name: "mode", Type: ArgEnum, Enum: []string{"a", "b"}, Default: "a"},
	}
	got, err := ParseArgs(decl, []string{"--tag", "x", "w1", "--count=3", "-n", "--tag=y"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": "w1", "count": int64(3), "tag": []any{"x", "y"}, "dry": true, "mode": "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, bad := range [][]string{{}, {"w", "--count", "x"}, {"w", "--mode", "c"}, {"w", "--nope"}, {"w", "extra"}} {
		if _, err := ParseArgs(decl, bad); err == nil {
			t.Errorf("%v should fail", bad)
		}
	}
	got, _ = ParseArgs(decl, []string{"--", "--literal"})
	if got["id"] != "--literal" {
		t.Fatalf("-- must end flag parsing: %v", got)
	}
}

func valid() string {
	return `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-x","version":"1.0.0","protocol_versions":[1],"executable":"bin/x",
	"commands":[{"name":"x","summary":"s","subcommands":[{"name":"y","summary":"s"}]}]}`
}

func TestManifestValidation(t *testing.T) {
	if _, err := ParseManifest([]byte(valid())); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := map[string]string{
		"reserved capability": `"capabilities":["authority.grant"],`,
		"unknown permission":  `"permissions":["root"],`,
		"unknown field":       `"surprise":true,`,
	}
	for name, ins := range bad {
		m := strings.Replace(valid(), `"commands"`, ins+`"commands"`, 1)
		if _, err := ParseManifest([]byte(m)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
	for name, m := range map[string]string{
		"escaping executable": strings.Replace(valid(), `"bin/x"`, `"../x"`, 1),
		"absolute executable": strings.Replace(valid(), `"bin/x"`, `"/bin/sh"`, 1),
		"bad name":            strings.Replace(valid(), `"gip-plugin-x"`, `"evil"`, 1),
		"bad version":         strings.Replace(valid(), `"1.0.0"`, `"latest"`, 1),
	} {
		if _, err := ParseManifest([]byte(m)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestExecutorManifest(t *testing.T) {
	base := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-executor","version":"0.1.0","protocol_versions":[1],"executable":"x",
	"capabilities":["execute.work"],"permissions":["work.read","work.execute","config.read","telemetry.emit"],
	"commands":[{"name":"execute","summary":"s","subcommands":[{"name":"next","summary":"s","permissions":["work.read","work.execute"],"timeout_seconds":%d}]}]}`
	if _, err := ParseManifest([]byte(fmt.Sprintf(base, 3600))); err != nil {
		t.Fatalf("executor manifest: %v", err)
	}
	for _, bad := range []int{-1, MaxCommandTimeoutSeconds + 1} {
		if _, err := ParseManifest([]byte(fmt.Sprintf(base, bad))); err == nil {
			t.Errorf("timeout %d must be refused", bad)
		}
	}
	group := `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-x","version":"0.1.0","protocol_versions":[1],"executable":"x",
	"commands":[{"name":"g","summary":"s","timeout_seconds":5,"subcommands":[{"name":"a","summary":"s"}]}]}`
	if _, err := ParseManifest([]byte(group)); err == nil {
		t.Error("a group cannot declare a timeout")
	}
}
