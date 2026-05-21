package modulebridge

import (
	"sort"

	"github.com/tjcran/boardgame-go/core"
)

// Op is one engine operation, exposed on both surfaces (Starlark
// ctx.modules.<Module>.<Name> and the MCP tool <MCPTool>).
//
// Call decodes args (already converted to plain Go values) against a
// live module state and returns a plain Go result. Determinism: an Op
// must be a pure function of (state, args, seeded RNG). Ops that read
// wall-clock or unseeded randomness must not be registered.
type Op struct {
	Module  string
	Name    string
	MCPTool string
	// Call receives ALL live module states (keyed by module name) so a
	// composing module (economy/shop over ccg) can reach another's state.
	// An op pulls its own module via modules[op.Module].
	// rng is the move's seeded PRNG (nil at design-time); ops that don't need randomness ignore it.
	Call func(modules map[string]any, args map[string]any, rng *core.Random) (any, error)
}

// emptyState is the placeholder a stateless module (economy, shop) puts
// in StarlarkG.Modules so its ctx.modules.<name> binding exists. Such
// modules operate on another module's state (ccg), not their own.
type emptyState struct{}

// Registry is the set of ops for one or more modules, built once at init.
type Registry struct {
	ops map[string][]Op
}

func NewRegistry() *Registry { return &Registry{ops: map[string][]Op{}} }

func (r *Registry) Add(op Op) { r.ops[op.Module] = append(r.ops[op.Module], op) }

// Ops returns the ops registered for a module (nil if none).
func (r *Registry) Ops(module string) []Op { return r.ops[module] }

// Modules returns every module name with at least one op.
func (r *Registry) Modules() []string {
	out := make([]string, 0, len(r.ops))
	for m := range r.ops {
		out = append(out, m)
	}
	return out
}

// NewState mints a fresh live state for a module by name. Returns nil
// for unknown modules. Used by Setup to populate StarlarkG.Modules.
func NewState(module string) any {
	if f := stateFactories[module]; f != nil {
		return f()
	}
	return nil
}

// stateFactories maps module name -> constructor. Populated by each
// module's binding file (e.g. ccg.go's init).
var stateFactories = map[string]func() any{}

// RegistryFor returns the op registry for a module name, or nil if the
// module is unknown. Each binding file registers itself here.
func RegistryFor(name string) *Registry { return registryByName[name] }

// AllModules returns every registered module name, sorted. This is the
// discovery entry point: it lets a client enumerate what module_op can
// reach without a live match or out-of-band docs.
func AllModules() []string {
	out := make([]string, 0, len(registryByName))
	for name := range registryByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var registryByName = map[string]*Registry{}
