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

	// EndsTurn controls whether the engine hands turn control to the
	// next player after this move runs successfully. Defaults to true —
	// matches the v0.4-v0.5.3 behaviour where every move ended the
	// turn. Setting "ends_turn": False in MOVES lets the same player
	// keep going (roll-then-build, draw-then-play, …) until they play
	// a move that's marked terminal.
	EndsTurn bool
}

// ArgDef is a single positional argument declaration.
type ArgDef struct {
	Name string
	Type string // "int", "string", "bool", "entity", "hex"
	Min  *int64
	Max  *int64

	// Zone, for Type=="entity", optionally constrains the entity to be in
	// this ccg zone at dispatch time. Empty means any registered entity.
	Zone string
}

// Phase is one entry in the spec's optional PHASES dict.
//
// Moves replaces the global MOVES table for the duration of the phase
// (matching core.PhaseConfig.Moves). EndIf is called after every move
// in this phase; returning a phase-name string transitions to that
// phase, None keeps the same phase active. Start: True marks the entry
// phase the game opens in — exactly one phase must set it.
type Phase struct {
	Name  string
	Moves map[string]Move
	EndIf starlark.Callable // optional
	Start bool
}

// Stage is one entry in the spec's optional STAGES dict.
//
// Moves replaces the active move table for a player gated into this
// stage via ctx.events.set_stage("stage_name"). Next, when set, is
// the stage a player transitions to when ctx.events.end_stage() runs
// from this stage; empty string means they leave the active set.
type Stage struct {
	Name  string
	Moves map[string]Move
	Next  string
}

// Hook binds a ccg event type to a Starlark handler fn(event, ctx).
type Hook struct {
	Type string
	Fn   starlark.Callable
}

// Spec is a compiled, structurally validated game spec.
type Spec struct {
	Meta       Meta
	Setup      starlark.Callable
	Moves      map[string]Move
	EndIf      starlark.Callable
	LegalMoves starlark.Callable
	PlayerView starlark.Callable // optional; nil → identity

	// Phases is the optional named phase table. When empty, the game has
	// no phase machinery and only the top-level Moves applies. When set,
	// exactly one phase must have Start: true; StartPhase holds its name.
	Phases     map[string]Phase
	StartPhase string

	// Stages is the optional top-level stage table (TurnConfig.Stages).
	// Players are gated into a stage via ctx.events.set_stage("name") from
	// inside an apply function; while gated, only the stage's Moves are
	// legal for that player. ctx.events.end_stage() pops them out.
	Stages map[string]Stage

	// Modules is the optional top-level MODULES list naming engine
	// modules the spec uses via ctx.modules.<name>.*. Empty when absent.
	Modules []string

	// Hooks is the optional ordered HOOKS table: ccg event type -> handler.
	// Requires "ccg" in Modules. Registered on the live ccg state in Setup.
	Hooks []Hook

	source string
}

// Source returns the original spec source text.
func (s *Spec) Source() string { return s.source }

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

	if err := readPhases(globals, s); err != nil {
		return nil, err
	}
	if err := readStages(globals, s); err != nil {
		return nil, err
	}
	if err := readModules(globals, s); err != nil {
		return nil, err
	}
	if err := readHooks(globals, s); err != nil {
		return nil, err
	}

	return s, nil
}

func readHooks(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["HOOKS"]
	if !ok {
		return nil
	}
	d, ok := raw.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("HOOKS must be a dict, got %s", raw.Type())
	}
	if d.Len() == 0 {
		return nil
	}
	hasCCG := false
	for _, m := range s.Modules {
		if m == "ccg" {
			hasCCG = true
		}
	}
	if !hasCCG {
		return fmt.Errorf("HOOKS requires \"ccg\" in MODULES")
	}
	for _, k := range d.Keys() {
		ks, ok := k.(starlark.String)
		if !ok {
			return fmt.Errorf("HOOKS key %v must be a string", k)
		}
		v, _, _ := d.Get(k)
		fn, ok := v.(starlark.Callable)
		if !ok {
			return fmt.Errorf("HOOKS[%q] must be a function, got %s", string(ks), v.Type())
		}
		s.Hooks = append(s.Hooks, Hook{Type: string(ks), Fn: fn})
	}
	return nil
}

var knownModules = map[string]bool{"ccg": true, "tabletop": true, "economy": true, "shop": true}

func readModules(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["MODULES"]
	if !ok {
		return nil
	}
	lst, ok := raw.(*starlark.List)
	if !ok {
		return fmt.Errorf("MODULES must be a list, got %s", raw.Type())
	}
	iter := lst.Iterate()
	defer iter.Done()
	var x starlark.Value
	for iter.Next(&x) {
		name, ok := x.(starlark.String)
		if !ok {
			return fmt.Errorf("MODULES entry %v must be a string", x)
		}
		if !knownModules[string(name)] {
			return fmt.Errorf("MODULES: unknown module %q", string(name))
		}
		s.Modules = append(s.Modules, string(name))
	}
	declared := map[string]bool{}
	for _, m := range s.Modules {
		declared[m] = true
	}
	for _, m := range s.Modules {
		if (m == "economy" || m == "shop") && !declared["ccg"] {
			return fmt.Errorf("MODULES: %q requires \"ccg\" to also be declared", m)
		}
	}
	return nil
}

// readStages parses the optional top-level STAGES dict.
//
// STAGES is a map from stage name to a dict containing:
//
//	moves  — required; same shape as MOVES (per-move ends_turn applies)
//	next   — optional string; stage to transition to on end_stage(),
//	         empty means leave the active set
//
// When STAGES is absent or empty, ctx.events.set_stage() is a no-op
// (no stage tables exist for the engine to gate on).
func readStages(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["STAGES"]
	if !ok {
		return nil
	}
	d, ok := raw.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("STAGES must be a dict, got %s", raw.Type())
	}
	if d.Len() == 0 {
		return nil
	}
	s.Stages = map[string]Stage{}
	for _, k := range d.Keys() {
		ks, ok := k.(starlark.String)
		if !ok {
			return fmt.Errorf("STAGES key %v must be a string", k)
		}
		name := string(ks)
		vAny, _, _ := d.Get(k)
		st, err := readStage(name, vAny)
		if err != nil {
			return fmt.Errorf("STAGES[%q]: %w", name, err)
		}
		s.Stages[name] = st
	}
	return nil
}

func readStage(name string, v starlark.Value) (Stage, error) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return Stage{}, fmt.Errorf("must be a dict")
	}
	st := Stage{Name: name, Moves: map[string]Move{}}

	movesAny, ok, _ := d.Get(starlark.String("moves"))
	if !ok {
		return Stage{}, errors.New("missing 'moves'")
	}
	movesDict, ok := movesAny.(*starlark.Dict)
	if !ok {
		return Stage{}, errors.New("'moves' must be a dict")
	}
	for _, mk := range movesDict.Keys() {
		mks, ok := mk.(starlark.String)
		if !ok {
			return Stage{}, fmt.Errorf("move key %v must be a string", mk)
		}
		mvRaw, _, _ := movesDict.Get(mk)
		mv, err := readMove(string(mks), mvRaw)
		if err != nil {
			return Stage{}, fmt.Errorf("moves[%q]: %w", string(mks), err)
		}
		st.Moves[mv.Name] = mv
	}
	if len(st.Moves) == 0 {
		return Stage{}, errors.New("'moves' must define at least one move")
	}

	if v, ok, _ := d.Get(starlark.String("next")); ok {
		s, ok := v.(starlark.String)
		if !ok {
			return Stage{}, errors.New("'next' must be a string")
		}
		st.Next = string(s)
	}

	return st, nil
}

// readPhases parses the optional top-level PHASES dict. PHASES is a
// map from phase name to a dict containing:
//
//	moves    — required; same shape as the top-level MOVES table
//	end_if   — optional callable (state, ctx) returning a phase-name
//	           string to transition, or None to stay in this phase
//	start    — optional bool; exactly one phase must have start=True
//
// When PHASES is absent or empty, the game has no phase machinery and
// the top-level Moves applies globally.
func readPhases(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["PHASES"]
	if !ok {
		return nil
	}
	d, ok := raw.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("PHASES must be a dict, got %s", raw.Type())
	}
	if d.Len() == 0 {
		return nil
	}
	s.Phases = map[string]Phase{}
	for _, k := range d.Keys() {
		ks, ok := k.(starlark.String)
		if !ok {
			return fmt.Errorf("PHASES key %v must be a string", k)
		}
		name := string(ks)
		vAny, _, _ := d.Get(k)
		ph, err := readPhase(name, vAny)
		if err != nil {
			return fmt.Errorf("PHASES[%q]: %w", name, err)
		}
		if ph.Start {
			if s.StartPhase != "" {
				return fmt.Errorf("PHASES: multiple phases marked start (%q and %q) — exactly one is allowed", s.StartPhase, name)
			}
			s.StartPhase = name
		}
		s.Phases[name] = ph
	}
	if s.StartPhase == "" {
		return errors.New("PHASES: exactly one phase must set \"start\": True")
	}
	return nil
}

func readPhase(name string, v starlark.Value) (Phase, error) {
	d, ok := v.(*starlark.Dict)
	if !ok {
		return Phase{}, fmt.Errorf("must be a dict")
	}
	ph := Phase{Name: name, Moves: map[string]Move{}}

	movesAny, ok, _ := d.Get(starlark.String("moves"))
	if !ok {
		return Phase{}, errors.New("missing 'moves'")
	}
	movesDict, ok := movesAny.(*starlark.Dict)
	if !ok {
		return Phase{}, errors.New("'moves' must be a dict")
	}
	for _, mk := range movesDict.Keys() {
		mks, ok := mk.(starlark.String)
		if !ok {
			return Phase{}, fmt.Errorf("move key %v must be a string", mk)
		}
		mvRaw, _, _ := movesDict.Get(mk)
		mv, err := readMove(string(mks), mvRaw)
		if err != nil {
			return Phase{}, fmt.Errorf("moves[%q]: %w", string(mks), err)
		}
		ph.Moves[mv.Name] = mv
	}
	if len(ph.Moves) == 0 {
		return Phase{}, errors.New("'moves' must define at least one move")
	}

	if v, ok, _ := d.Get(starlark.String("end_if")); ok {
		c, ok := v.(starlark.Callable)
		if !ok {
			return Phase{}, errors.New("'end_if' must be callable")
		}
		ph.EndIf = c
	}

	if v, ok, _ := d.Get(starlark.String("start")); ok {
		b, ok := v.(starlark.Bool)
		if !ok {
			return Phase{}, errors.New("'start' must be a bool")
		}
		ph.Start = bool(b)
	}

	return ph, nil
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

	m := Move{Name: name, Apply: apply, EndsTurn: true}
	if v, ok, _ := d.Get(starlark.String("ends_turn")); ok {
		b, ok := v.(starlark.Bool)
		if !ok {
			return Move{}, fmt.Errorf("'ends_turn' must be a bool, got %s", v.Type())
		}
		m.EndsTurn = bool(b)
	}
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
	if z, ok := pickString("zone"); ok {
		a.Zone = z
	}
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
