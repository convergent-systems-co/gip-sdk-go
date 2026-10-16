package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseArgs binds command-line words to a command's declared arguments.
// Flags may appear anywhere ("--name value", "--name=value", "-s value",
// boolean "--flag"); "--" ends flag parsing. Positional words bind in
// declaration order. The result is keyed by argument name and typed:
// string, int64, float64, bool, or []any for repeated arguments.
//
// The host uses the same parser for built-in and plugin commands, so every
// command on the gip CLI follows one convention.
func ParseArgs(decl []Argument, words []string) (map[string]any, error) {
	byName := map[string]*Argument{}
	byShort := map[string]*Argument{}
	var positionals []*Argument
	for i := range decl {
		a := &decl[i]
		if a.Positional {
			positionals = append(positionals, a)
			continue
		}
		byName[a.Name] = a
		if a.Short != "" {
			byShort[a.Short] = a
		}
	}

	out := map[string]any{}
	var loose []string
	set := func(a *Argument, raw string) error {
		v, err := convert(a, raw)
		if err != nil {
			return err
		}
		if a.Repeated {
			prev, _ := out[a.Name].([]any)
			out[a.Name] = append(prev, v)
			return nil
		}
		if _, dup := out[a.Name]; dup {
			return fmt.Errorf("--%s given more than once", a.Name)
		}
		out[a.Name] = v
		return nil
	}

	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			loose = append(loose, words[i+1:]...)
			break
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			loose = append(loose, w)
			continue
		}
		var a *Argument
		name, value, hasValue := strings.Cut(strings.TrimLeft(w, "-"), "=")
		if strings.HasPrefix(w, "--") {
			a = byName[name]
		} else if len(name) == 1 {
			a = byShort[name]
		}
		if a == nil {
			return nil, fmt.Errorf("unknown flag %s", w)
		}
		if a.Type == ArgBoolean {
			if !hasValue {
				value = "true"
			}
			if err := set(a, value); err != nil {
				return nil, err
			}
			continue
		}
		if !hasValue {
			if i+1 >= len(words) {
				return nil, fmt.Errorf("flag --%s needs a value", a.Name)
			}
			i++
			value = words[i]
		}
		if err := set(a, value); err != nil {
			return nil, err
		}
	}

	pi := 0
	for _, w := range loose {
		if pi >= len(positionals) {
			return nil, fmt.Errorf("unexpected argument %q", w)
		}
		a := positionals[pi]
		if err := set(a, w); err != nil {
			return nil, err
		}
		if !a.Repeated {
			pi++
		}
	}

	for i := range decl {
		a := &decl[i]
		if _, ok := out[a.Name]; ok {
			continue
		}
		if a.Default != nil {
			out[a.Name] = a.Default
			continue
		}
		if a.Required {
			if a.Positional {
				return nil, fmt.Errorf("missing required argument <%s>", a.Name)
			}
			return nil, fmt.Errorf("missing required flag --%s", a.Name)
		}
	}
	return out, nil
}

// ValidateArgs checks an already-decoded argument map (for example one
// received over the protocol) against the declaration.
func ValidateArgs(decl []Argument, args map[string]any) error {
	known := map[string]*Argument{}
	for i := range decl {
		known[decl[i].Name] = &decl[i]
	}
	for k := range args {
		if known[k] == nil {
			return fmt.Errorf("unknown argument %q", k)
		}
	}
	for _, a := range decl {
		v, ok := args[a.Name]
		if !ok {
			if a.Required && a.Default == nil {
				return fmt.Errorf("missing required argument %q", a.Name)
			}
			continue
		}
		vals := []any{v}
		if a.Repeated {
			list, ok := v.([]any)
			if !ok {
				return fmt.Errorf("argument %q must be a list", a.Name)
			}
			vals = list
		}
		for _, x := range vals {
			if err := checkType(&a, x); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkType(a *Argument, v any) error {
	switch a.Type {
	case ArgString:
		if _, ok := v.(string); ok {
			return nil
		}
	case ArgEnum:
		if s, ok := v.(string); ok && contains(a.Enum, s) {
			return nil
		}
	case ArgBoolean:
		if _, ok := v.(bool); ok {
			return nil
		}
	case ArgInteger:
		switch n := v.(type) {
		case int64, int:
			return nil
		case float64:
			if n == float64(int64(n)) {
				return nil
			}
		}
	case ArgNumber:
		switch v.(type) {
		case float64, int64, int:
			return nil
		}
	}
	return fmt.Errorf("argument %q: value %v is not a valid %s", a.Name, v, a.Type)
}

func convert(a *Argument, raw string) (any, error) {
	switch a.Type {
	case ArgString:
		return raw, nil
	case ArgEnum:
		if !contains(a.Enum, raw) {
			return nil, fmt.Errorf("--%s must be one of %s", a.Name, strings.Join(a.Enum, ", "))
		}
		return raw, nil
	case ArgBoolean:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("--%s must be true or false", a.Name)
		}
		return b, nil
	case ArgInteger:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s must be an integer", a.Name)
		}
		return n, nil
	case ArgNumber:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s must be a number", a.Name)
		}
		return f, nil
	}
	return nil, fmt.Errorf("argument %q has unknown type %q", a.Name, a.Type)
}
