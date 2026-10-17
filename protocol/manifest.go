// Package protocol defines the versioned contract between the gip host and
// out-of-process plugins: the static manifest a plugin ships, and the
// JSON-RPC 2.0 messages exchanged over the plugin's stdin/stdout.
//
// This package depends only on the Go standard library so that plugin
// repositories can import it without importing the gip implementation.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Version is the plugin protocol version implemented by this package.
const Version = 1

// ManifestFile is the file name the host reads during discovery. Discovery
// reads only this file; it never executes plugin code.
const ManifestFile = "gip-plugin.json"

// ManifestSchema identifies the manifest format.
const ManifestSchema = "gip.plugin-manifest/v1"

// MaxManifestBytes bounds how much the host will read during discovery.
const MaxManifestBytes = 1 << 20

var (
	pluginNameRE  = regexp.MustCompile(`^gip-plugin-[a-z0-9][a-z0-9-]{0,62}$`)
	commandNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	argNameRE     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	semverRE      = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	attrKeyRE     = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)
)

// Manifest is the static, declarative description of a plugin.
type Manifest struct {
	Schema           string    `json:"schema"`
	Name             string    `json:"name"`
	Version          string    `json:"version"`
	Description      string    `json:"description,omitempty"`
	ProtocolVersions []int     `json:"protocol_versions"`
	Executable       string    `json:"executable"`
	Capabilities     []string  `json:"capabilities,omitempty"`
	Permissions      []string  `json:"permissions,omitempty"`
	Commands         []Command `json:"commands"`
	Telemetry        Telemetry `json:"telemetry,omitempty"`
}

// Command is a command or command group a plugin contributes to the gip CLI.
// A command with Subcommands is a group and cannot be invoked itself.
type Command struct {
	Name         string          `json:"name"`
	Summary      string          `json:"summary"`
	Arguments    []Argument      `json:"arguments,omitempty"`
	Permissions  []string        `json:"permissions,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Subcommands  []Command       `json:"subcommands,omitempty"`
	// TimeoutSeconds is how long one invocation may run, when longer than
	// the host's default (for example an executor driving a model). The
	// host may cap it.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Argument declares one typed command argument. Positional arguments are
// bound in declaration order; the rest are --flags.
type Argument struct {
	Name       string   `json:"name"`
	Type       ArgType  `json:"type"`
	Summary    string   `json:"summary,omitempty"`
	Required   bool     `json:"required,omitempty"`
	Positional bool     `json:"positional,omitempty"`
	Repeated   bool     `json:"repeated,omitempty"`
	Enum       []string `json:"enum,omitempty"`
	Default    any      `json:"default,omitempty"`
	Short      string   `json:"short,omitempty"`
}

// ArgType is the type of a command argument.
type ArgType string

const (
	ArgString  ArgType = "string"
	ArgInteger ArgType = "integer"
	ArgNumber  ArgType = "number"
	ArgBoolean ArgType = "boolean"
	ArgEnum    ArgType = "enum"
)

// Telemetry declares plugin-specific telemetry attribute keys. Keys must be
// namespaced under "gip.plugin.<short-name>.". Undeclared keys are dropped
// by the host.
type Telemetry struct {
	Attributes []string `json:"attributes,omitempty"`
}

// Capabilities a plugin may promise. A capability is a statement about the
// plugin's own behaviour; it is never a grant of authority.
const (
	CapProposeIntentInterpretation = "propose.interpretation"
	CapProposeGoals                = "propose.goals"
	CapProposeInvariants           = "propose.invariants"
	CapProposeWork                 = "propose.work"
	CapProposeGraphRules           = "propose.graph-rules"
	CapProduceEvidence             = "produce.evidence"
	CapExecuteWork                 = "execute.work"
	CapVisualize                   = "visualize"
)

// KnownCapabilities is the closed set accepted by protocol v1.
var KnownCapabilities = []string{
	CapProposeIntentInterpretation, CapProposeGoals, CapProposeInvariants,
	CapProposeWork, CapProposeGraphRules, CapProduceEvidence, CapExecuteWork,
	CapVisualize,
}

// ReservedCapabilityPrefixes name powers only the core holds. A manifest
// declaring one is malformed: availability never confers authority.
var ReservedCapabilityPrefixes = []string{
	"authority.", "admit.", "accept.", "qualify.", "ledger.", "governance.",
}

// Permissions a plugin may request. Only host-mediated permissions are
// enforced by the host; see docs/spec/plugin-protocol.md.
const (
	PermTelemetryEmit   = "telemetry.emit"
	PermStateRead       = "state.read"
	PermIntentRead      = "intent.read"
	PermProposalsSubmit = "proposals.submit"
	PermEvidenceSubmit  = "evidence.submit"
	PermPluginData      = "plugin-data"
	// Executor permissions (SDK v0.2.0).
	PermWorkRead    = "work.read"    // frontier and work contracts
	PermWorkExecute = "work.execute" // act on work as a trusted agent principal; host-run checks
	PermConfigRead  = "config.read"  // the plugin's own <config>/<name>.yaml
	// PermPlanWrite (SDK v0.4.0) is declared in plan.go.
)

// KnownPermissions is the closed set accepted by protocol v1.
var KnownPermissions = []string{
	PermTelemetryEmit, PermStateRead, PermIntentRead, PermProposalsSubmit,
	PermEvidenceSubmit, PermPluginData, PermWorkRead, PermWorkExecute, PermConfigRead,
	PermPlanWrite,
}

// MaxCommandTimeoutSeconds bounds a command's declared timeout.
const MaxCommandTimeoutSeconds = 7200

// ShortName returns the plugin name without the "gip-plugin-" prefix.
func (m *Manifest) ShortName() string { return strings.TrimPrefix(m.Name, "gip-plugin-") }

// SupportsVersion reports whether the manifest lists protocol version v.
func (m *Manifest) SupportsVersion(v int) bool {
	for _, x := range m.ProtocolVersions {
		if x == v {
			return true
		}
	}
	return false
}

// ParseManifest decodes and validates a manifest. Unknown fields are
// rejected so that a misspelt permission or capability cannot be ignored.
func ParseManifest(data []byte) (*Manifest, error) {
	if len(data) > MaxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", MaxManifestBytes)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if dec.More() {
		return nil, errors.New("decode manifest: trailing data")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks structural validity. It does not check protocol
// compatibility with a particular host; see SupportsVersion.
func (m *Manifest) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if m.Schema != ManifestSchema {
		add("schema must be %q, got %q", ManifestSchema, m.Schema)
	}
	if !pluginNameRE.MatchString(m.Name) {
		add("name %q must match %s", m.Name, pluginNameRE)
	}
	if !semverRE.MatchString(m.Version) {
		add("version %q is not a semantic version", m.Version)
	}
	if len(m.ProtocolVersions) == 0 {
		add("protocol_versions must not be empty")
	}
	for _, v := range m.ProtocolVersions {
		if v < 1 {
			add("protocol version %d is invalid", v)
		}
	}
	if err := validateExecutable(m.Executable); err != nil {
		errs = append(errs, err)
	}
	for _, c := range m.Capabilities {
		if reservedCapability(c) {
			add("capability %q is reserved to the gip core", c)
		} else if !contains(KnownCapabilities, c) {
			add("capability %q is unknown to protocol v%d", c, Version)
		}
	}
	if dup := firstDuplicate(m.Capabilities); dup != "" {
		add("capability %q declared twice", dup)
	}
	for _, p := range m.Permissions {
		if !contains(KnownPermissions, p) {
			add("permission %q is unknown to protocol v%d", p, Version)
		}
	}
	if dup := firstDuplicate(m.Permissions); dup != "" {
		add("permission %q declared twice", dup)
	}
	if len(m.Commands) == 0 {
		add("a plugin must declare at least one command")
	}
	seen := map[string]bool{}
	for i := range m.Commands {
		c := &m.Commands[i]
		if seen[c.Name] {
			add("command %q declared twice", c.Name)
		}
		seen[c.Name] = true
		errs = append(errs, validateCommand(c, []string{c.Name}, m.Permissions)...)
	}
	prefix := "gip.plugin." + strings.ReplaceAll(m.ShortName(), "-", "_") + "."
	for _, k := range m.Telemetry.Attributes {
		if !attrKeyRE.MatchString(k) || !strings.HasPrefix(k, prefix) {
			add("telemetry attribute %q must match %s and start with %q", k, attrKeyRE, prefix)
		}
	}
	return errors.Join(errs...)
}

func validateExecutable(p string) error {
	switch {
	case p == "":
		return errors.New("executable must not be empty")
	case strings.Contains(p, "\\"):
		return fmt.Errorf("executable %q must use forward slashes", p)
	case path.IsAbs(p) || strings.HasPrefix(p, "/"):
		return fmt.Errorf("executable %q must be relative to the plugin directory", p)
	case path.Clean(p) != p || strings.HasPrefix(p, "..") || strings.Contains(p, "/../"):
		return fmt.Errorf("executable %q must be a clean path inside the plugin directory", p)
	}
	return nil
}

func validateCommand(c *Command, cmdPath []string, pluginPerms []string) []error {
	var errs []error
	add := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("command %q: "+format, append([]any{strings.Join(cmdPath, " ")}, a...)...))
	}
	if !commandNameRE.MatchString(c.Name) {
		add("name must match %s", commandNameRE)
	}
	if strings.TrimSpace(c.Summary) == "" {
		add("summary must not be empty")
	}
	for _, p := range c.Permissions {
		if !contains(pluginPerms, p) {
			add("permission %q is not declared at plugin level", p)
		}
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > MaxCommandTimeoutSeconds {
		add("timeout_seconds must be between 0 and %d", MaxCommandTimeoutSeconds)
	}
	if len(c.Subcommands) > 0 {
		if len(c.Arguments) > 0 || len(c.OutputSchema) > 0 || len(c.Permissions) > 0 || c.TimeoutSeconds != 0 {
			add("a command group cannot declare arguments, permissions, an output schema or a timeout")
		}
		seen := map[string]bool{}
		for i := range c.Subcommands {
			s := &c.Subcommands[i]
			if seen[s.Name] {
				add("subcommand %q declared twice", s.Name)
			}
			seen[s.Name] = true
			errs = append(errs, validateCommand(s, append(append([]string{}, cmdPath...), s.Name), pluginPerms)...)
		}
		return errs
	}
	if len(c.OutputSchema) > 0 && !json.Valid(c.OutputSchema) {
		add("output_schema is not valid JSON")
	}
	names := map[string]bool{}
	sawOptionalPositional, sawRepeatedPositional := false, false
	for _, a := range c.Arguments {
		if !argNameRE.MatchString(a.Name) {
			add("argument %q must match %s", a.Name, argNameRE)
		}
		if names[a.Name] {
			add("argument %q declared twice", a.Name)
		}
		names[a.Name] = true
		switch a.Type {
		case ArgString, ArgInteger, ArgNumber, ArgBoolean:
			if len(a.Enum) > 0 {
				add("argument %q: enum values require type enum", a.Name)
			}
		case ArgEnum:
			if len(a.Enum) == 0 {
				add("argument %q: type enum requires enum values", a.Name)
			}
		default:
			add("argument %q: unknown type %q", a.Name, a.Type)
		}
		if a.Short != "" && (len(a.Short) != 1 || a.Positional) {
			add("argument %q: short must be one character on a flag", a.Name)
		}
		if a.Positional {
			if a.Type == ArgBoolean {
				add("argument %q: a boolean cannot be positional", a.Name)
			}
			if sawRepeatedPositional {
				add("argument %q: no positional may follow a repeated positional", a.Name)
			}
			if a.Required && sawOptionalPositional {
				add("argument %q: a required positional cannot follow an optional one", a.Name)
			}
			sawOptionalPositional = sawOptionalPositional || !a.Required
			sawRepeatedPositional = sawRepeatedPositional || a.Repeated
		}
	}
	return errs
}

// Leaves returns every invocable command path in a stable order.
func (m *Manifest) Leaves() [][]string {
	var out [][]string
	var walk func(prefix []string, cs []Command)
	walk = func(prefix []string, cs []Command) {
		for _, c := range cs {
			p := append(append([]string{}, prefix...), c.Name)
			if len(c.Subcommands) == 0 {
				out = append(out, p)
			} else {
				walk(p, c.Subcommands)
			}
		}
	}
	walk(nil, m.Commands)
	sort.Slice(out, func(i, j int) bool { return strings.Join(out[i], " ") < strings.Join(out[j], " ") })
	return out
}

// Find returns the command at path, or nil.
func (m *Manifest) Find(cmdPath []string) *Command {
	cs := m.Commands
	var found *Command
	for _, name := range cmdPath {
		found = nil
		for i := range cs {
			if cs[i].Name == name {
				found = &cs[i]
				break
			}
		}
		if found == nil {
			return nil
		}
		cs = found.Subcommands
	}
	return found
}

func reservedCapability(c string) bool {
	for _, p := range ReservedCapabilityPrefixes {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func firstDuplicate(xs []string) string {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			return x
		}
		seen[x] = true
	}
	return ""
}
