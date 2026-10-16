package protocol

import (
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
