package starlarkgame

import (
	"errors"
	"fmt"
	"regexp"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Meta is the strongly-typed view of the spec's top-level META dict.
type Meta struct {
	Name        string
	MinPlayers  int
	MaxPlayers  int
	Description string
}

// Move is one entry in the spec's MOVES dict.
type Move struct {
	Name    string
	ArgsDef []ArgDef
	Apply   starlark.Callable
}

// ArgDef is a single positional argument declaration.
type ArgDef struct {
	Name string
	Type string // "int", "string", "bool"
	Min  *int64
	Max  *int64
}

// Spec is a compiled, structurally validated game spec.
type Spec struct {
	Meta       Meta
	Setup      starlark.Callable
	Moves      map[string]Move
	EndIf      starlark.Callable
	LegalMoves starlark.Callable
	PlayerView starlark.Callable // optional; nil → identity

	source string
}

var nameRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// LoadSpec compiles and structurally validates a spec source. It does NOT
// run setup / legal_moves / end_if smoke checks — those run in Validate
// (Task 12). Returning a Spec only guarantees: syntax OK, required top-
// level globals present with correct types, META well-formed.
func LoadSpec(source string) (*Spec, error) {
	thread := &starlark.Thread{
		Name: "load-spec",
		Load: func(*starlark.Thread, string) (starlark.StringDict, error) {
			return nil, errors.New("load() is disabled in game specs")
		},
		Print: func(*starlark.Thread, string) {},
	}
	thread.SetMaxExecutionSteps(uint64(defaultLimits().MaxSteps))

	// Top-level control flow is needed because real game specs use
	// for/if at module level. Same as evalSandbox.
	opts := &syntax.FileOptions{TopLevelControl: true}
	globals, err := starlark.ExecFileOptions(opts, thread, "spec.star", source, nil)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	s := &Spec{source: source, Moves: map[string]Move{}}

	if err := readMeta(globals, &s.Meta); err != nil {
		return nil, err
	}
	if !nameRe.MatchString(s.Meta.Name) {
		return nil, fmt.Errorf("META.name %q must match %s", s.Meta.Name, nameRe)
	}

	for _, want := range []struct {
		name string
		into *starlark.Callable
	}{
		{"setup", &s.Setup},
		{"end_if", &s.EndIf},
		{"legal_moves", &s.LegalMoves},
	} {
		v, ok := globals[want.name]
		if !ok {
			return nil, fmt.Errorf("missing required top-level function %q", want.name)
		}
		c, ok := v.(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("%q must be a function, got %s", want.name, v.Type())
		}
		*want.into = c
	}

	if v, ok := globals["player_view"]; ok {
		c, ok := v.(starlark.Callable)
		if !ok {
			return nil, fmt.Errorf("player_view must be a function")
		}
		s.PlayerView = c
	}

	movesAny, ok := globals["MOVES"]
	if !ok {
		return nil, errors.New("missing required top-level dict MOVES")
	}
	movesDict, ok := movesAny.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("MOVES must be a dict, got %s", movesAny.Type())
	}
	for _, k := range movesDict.Keys() {
		ks, ok := k.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("MOVES key %v must be a string", k)
		}
		vAny, _, _ := movesDict.Get(k)
		mv, err := readMove(string(ks), vAny)
		if err != nil {
			return nil, fmt.Errorf("MOVES[%q]: %w", string(ks), err)
		}
		s.Moves[mv.Name] = mv
	}
	if len(s.Moves) == 0 {
		return nil, errors.New("MOVES must define at least one move")
	}
	return s, nil
}

func readMeta(globals starlark.StringDict, into *Meta) error {
	v, ok := globals["META"]
	if !ok {
		return errors.New("missing required top-level dict META")
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("META must be a dict, got %s", v.Type())
	}

	pickString := func(key string) (string, error) {
		val, ok, _ := d.Get(starlark.String(key))
		if !ok {
			return "", fmt.Errorf("META.%s missing", key)
		}
		s, ok := val.(starlark.String)
		if !ok {
			return "", fmt.Errorf("META.%s must be string", key)
		}
		return string(s), nil
	}
	pickInt := func(key string) (int, error) {
		val, ok, _ := d.Get(starlark.String(key))
		if !ok {
			return 0, fmt.Errorf("META.%s missing", key)
		}
		i, ok := val.(starlark.Int)
		if !ok {
			return 0, fmt.Errorf("META.%s must be int", key)
		}
		n, _ := i.Int64()
		return int(n), nil
	}

	var err error
	if into.Name, err = pickString("name"); err != nil {
		return err
	}
	if into.MinPlayers, err = pickInt("min_players"); err != nil {
		return err
	}
	if into.MaxPlayers, err = pickInt("max_players"); err != nil {
		return err
	}
	if v, _, _ := d.Get(starlark.String("description")); v != nil {
		if s, ok := v.(starlark.String); ok {
			into.Description = string(s)
		}
	}
	if into.MinPlayers < 1 || into.MaxPlayers < into.MinPlayers || into.MaxPlayers > 16 {
		return fmt.Errorf("META: require 1 <= min_players (%d) <= max_players (%d) <= 16", into.MinPlayers, into.MaxPlayers)
	}
	return nil
}

func readMove(name string, v starlark.Value) (Move, error) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return Move{}, fmt.Errorf("must be a dict")
	}
	applyAny, ok, _ := d.Get(starlark.String("apply"))
	if !ok {
		return Move{}, errors.New("missing 'apply'")
	}
	apply, ok := applyAny.(starlark.Callable)
	if !ok {
		return Move{}, errors.New("'apply' must be callable")
	}

	m := Move{Name: name, Apply: apply}
	if argsAny, ok, _ := d.Get(starlark.String("args")); ok {
		argsList, ok := argsAny.(*starlark.List)
		if !ok {
			return Move{}, errors.New("'args' must be a list")
		}
		iter := argsList.Iterate()
		defer iter.Done()
		var x starlark.Value
		for iter.Next(&x) {
			ad, err := readArgDef(x)
			if err != nil {
				return Move{}, err
			}
			m.ArgsDef = append(m.ArgsDef, ad)
		}
	}
	return m, nil
}

func readArgDef(v starlark.Value) (ArgDef, error) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return ArgDef{}, fmt.Errorf("arg def must be a dict")
	}
	pickString := func(k string) (string, bool) {
		v, ok, _ := d.Get(starlark.String(k))
		if !ok {
			return "", false
		}
		s, ok := v.(starlark.String)
		if !ok {
			return "", false
		}
		return string(s), true
	}
	name, _ := pickString("name")
	typ, _ := pickString("type")
	a := ArgDef{Name: name, Type: typ}
	if v, ok, _ := d.Get(starlark.String("min")); ok {
		if i, ok := v.(starlark.Int); ok {
			n, _ := i.Int64()
			a.Min = &n
		}
	}
	if v, ok, _ := d.Get(starlark.String("max")); ok {
		if i, ok := v.(starlark.Int); ok {
			n, _ := i.Int64()
			a.Max = &n
		}
	}
	return a, nil
}
