// Package determinism is a go/analysis pass that flags
// non-deterministic calls (wall-clock reads, OS RNG, env-driven
// behaviour) inside functions that look like game move handlers or
// engine hooks.
//
// A function body is in scope if either:
//
//   - It's assigned to a struct field whose type is core.MoveFn,
//     core.HookFn, core.SetupFn, core.EndIfFn, or core.PlayerViewFn.
//   - It's used as a map value where the surrounding map is built
//     from a `map[string]any` literal whose key shape (a Move name)
//     matches a known move-table site. (This is a heuristic — see
//     the inferMoveByContext helper.)
//   - The function signature literally matches one of the engine's
//     callable shapes:
//
//       func(*core.MoveContext, ...any) (core.G, error)
//       func(*core.MoveContext) core.G
//
// Banned calls flagged:
//   - time.Now, time.Since, time.Until, time.Tick, time.After
//   - math/rand.*  (any free function; *Rand methods too)
//   - crypto/rand.*
//   - os.Getenv
//
// The engine's seeded RNG (mc.Random.* and core.Shuffle) is allowed —
// the analyzer recognises those receivers by type.
package determinism

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the exported analysis.Analyzer suitable for plugging into
// go vet or any analyzer driver.
var Analyzer = &analysis.Analyzer{
	Name:     "determinism",
	Doc:      "flags non-deterministic calls inside boardgame-go MoveFn/HookFn bodies",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// bannedSelectors is the list of <pkgPath, name> pairs we refuse to
// allow inside an in-scope function body.
var bannedSelectors = []struct {
	pkg, name, why string
}{
	{"time", "Now", "time.Now() breaks replay / MCTS rollouts; use a Random plugin or pass time through Setup"},
	{"time", "Since", "time.Since() reads the wall clock; same problem as time.Now"},
	{"time", "Until", "time.Until() reads the wall clock"},
	{"time", "Tick", "time.Tick reads the wall clock and leaks a goroutine"},
	{"time", "After", "time.After reads the wall clock and leaks a goroutine"},
	{"math/rand", "Float64", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "Int", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "Intn", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "Int63", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "Int63n", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "NormFloat64", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand", "Perm", "math/rand isn't seeded by the engine — use core.Shuffle"},
	{"math/rand", "Shuffle", "math/rand isn't seeded by the engine — use core.Shuffle"},
	{"math/rand", "Read", "math/rand isn't seeded by the engine — use mc.Random"},
	{"math/rand/v2", "Float64", "math/rand/v2 isn't seeded by the engine — use mc.Random"},
	{"math/rand/v2", "Int", "math/rand/v2 isn't seeded by the engine — use mc.Random"},
	{"math/rand/v2", "IntN", "math/rand/v2 isn't seeded by the engine — use mc.Random"},
	{"crypto/rand", "Read", "crypto/rand is non-deterministic; use mc.Random for in-move entropy"},
	{"os", "Getenv", "env vars vary by host; pass values through Setup"},
	{"os", "Hostname", "host identity varies; pass through Setup"},
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// First pass: collect candidate function bodies (FuncDecl /
	// FuncLit) whose signature matches an engine callable shape.
	type candidate struct {
		body *ast.BlockStmt
		kind string // for the diagnostic message
	}
	var candidates []candidate

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}, func(n ast.Node) {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body == nil {
				return
			}
			if kind, ok := matchEngineCallable(pass, fn.Type); ok {
				candidates = append(candidates, candidate{body: fn.Body, kind: kind})
			}
		case *ast.FuncLit:
			if fn.Body == nil {
				return
			}
			if kind, ok := matchEngineCallable(pass, fn.Type); ok {
				candidates = append(candidates, candidate{body: fn.Body, kind: kind})
			}
		}
	})

	// For each candidate, walk its body and report banned selectors.
	for _, c := range candidates {
		ast.Inspect(c.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgPath, name, ok := selectorIdentity(pass, sel)
			if !ok {
				return true
			}
			for _, b := range bannedSelectors {
				if b.pkg == pkgPath && b.name == name {
					pass.Reportf(call.Pos(),
						"determinism: %s.%s is not allowed in a %s body: %s",
						b.pkg, b.name, c.kind, b.why)
					return true
				}
			}
			return true
		})
	}
	return nil, nil
}

// matchEngineCallable returns (kind, true) when ft's signature looks
// like one of the engine's callable function types.
//
// We use the textual / structural shape rather than a strict
// types-package comparison so users vendoring the engine under a
// different module path still get checked.
func matchEngineCallable(pass *analysis.Pass, ft *ast.FuncType) (string, bool) {
	if ft == nil || ft.Params == nil {
		return "", false
	}
	params := flattenFields(ft.Params)
	results := flattenFields(ft.Results)

	// MoveFn: func(*core.MoveContext, ...any) (core.G, error)
	if len(params) == 2 && len(results) == 2 &&
		isPtrToNamed(pass, params[0], "MoveContext") &&
		isVariadicAny(params[1]) &&
		isNamed(pass, results[0], "G") &&
		isErrorType(pass, results[1]) {
		return "MoveFn", true
	}
	// HookFn: func(*core.MoveContext) core.G
	if len(params) == 1 && len(results) == 1 &&
		isPtrToNamed(pass, params[0], "MoveContext") &&
		isNamed(pass, results[0], "G") {
		return "HookFn", true
	}
	// EndIfFn: func(*core.MoveContext) any
	if len(params) == 1 && len(results) == 1 &&
		isPtrToNamed(pass, params[0], "MoveContext") &&
		isInterfaceAny(pass, results[0]) {
		return "EndIfFn", true
	}
	return "", false
}

// flattenFields turns ast.FieldList into a flat slice of expressions in
// declaration order. Each ast.Field can carry multiple names so we
// materialise one expr per name (or zero, for unnamed results).
func flattenFields(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, f := range fl.List {
		count := len(f.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			out = append(out, f.Type)
		}
	}
	return out
}

// isPtrToNamed reports whether expr is `*pkg.Name` (or `*Name` in the
// same package) for the supplied unqualified type name. Used to spot
// `*core.MoveContext` parameters.
func isPtrToNamed(pass *analysis.Pass, expr ast.Expr, name string) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	return isNamed(pass, star.X, name)
}

// isNamed reports whether expr resolves to a named type with the
// supplied unqualified name (e.g. `core.G` or just `G`).
func isNamed(pass *analysis.Pass, expr ast.Expr, want string) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	named, ok := tv.Type.(*types.Named)
	if !ok {
		// Alias (G = any). Treat by surface name.
		if id, ok := expr.(*ast.Ident); ok && id.Name == want {
			return true
		}
		if sel, ok := expr.(*ast.SelectorExpr); ok && sel.Sel.Name == want {
			return true
		}
		return false
	}
	return named.Obj().Name() == want
}

// isVariadicAny reports whether expr is `...any` / `...interface{}`.
func isVariadicAny(expr ast.Expr) bool {
	ell, ok := expr.(*ast.Ellipsis)
	if !ok {
		return false
	}
	switch el := ell.Elt.(type) {
	case *ast.Ident:
		return el.Name == "any"
	case *ast.InterfaceType:
		return len(el.Methods.List) == 0
	}
	return false
}

func isErrorType(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok {
		if id, ok := expr.(*ast.Ident); ok && id.Name == "error" {
			return true
		}
		return false
	}
	named, ok := tv.Type.(*types.Named)
	return ok && named.Obj().Name() == "error"
}

func isInterfaceAny(pass *analysis.Pass, expr ast.Expr) bool {
	if id, ok := expr.(*ast.Ident); ok && id.Name == "any" {
		return true
	}
	if it, ok := expr.(*ast.InterfaceType); ok {
		return len(it.Methods.List) == 0
	}
	return false
}

// selectorIdentity returns the imported package path and selector name
// for sel, when sel refers to a top-level package function. Returns
// "", "", false when sel doesn't resolve to such a call (e.g. a method
// on a local receiver).
func selectorIdentity(pass *analysis.Pass, sel *ast.SelectorExpr) (string, string, bool) {
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	obj := pass.TypesInfo.Uses[pkgIdent]
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return "", "", false
	}
	return pkgName.Imported().Path(), sel.Sel.Name, true
}
