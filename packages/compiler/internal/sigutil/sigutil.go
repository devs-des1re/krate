// Package sigutil centralizes detection of reactive declarations
// (createSignal / createResource destructuring) across the compiler. The
// annotator and the IR builder previously walked function bodies with their own
// copies of this traversal; both now consume this single implementation.
package sigutil

import "github.com/kratejs/krate/packages/compiler/ast"

// CSSKind identifies which zero-JS CSS primitive a declaration uses.
type CSSKind int

const (
	// CSSKindNone is a non-CSS reactive declaration (createSignal/createResource).
	CSSKindNone CSSKind = iota
	// CSSKindChoice is `createCSSChoice(initial, options?)` — a radio group.
	CSSKindChoice
	// CSSKindToggle is `createCSSToggle(initial)` — a single checkbox.
	CSSKindToggle
	// CSSKindFlags is `createCSSFlags([...])` — independent checkboxes.
	CSSKindFlags
)

// Decl is a single detected reactive declaration.
type Decl struct {
	Name       string
	Setter     string   // createSignal's second destructured name
	Initial    ast.Expr // initial value expression (may be nil for resources)
	IsResource bool
	// CSSKind marks a zero-JS CSS declaration (choice/toggle/flags). These are
	// compiled to hidden inputs + `:has()` CSS, never emitted as JS signals.
	CSSKind CSSKind
	// Options holds the option universe: the explicit array for a choice, the
	// flag names for flags, or ["on"] for a toggle. Empty for non-CSS decls.
	Options []string
}

// Find walks a statement list for reactive declarations. When recurse is true,
// control-flow bodies and nested blocks are walked too (the annotator's
// scoping); when false only top-level statements are inspected (the IR
// builder's scoping — signals declared in blocks are out of hydration scope).
func Find(body []ast.Stmt, recurse bool) []Decl {
	var out []Decl
	var walk func([]ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.VarStmt:
				for _, decl := range s.Decls {
					if !decl.IsDestructuring || decl.Init == nil {
						continue
					}
					call, ok := decl.Init.(*ast.CallExpr)
					if !ok {
						continue
					}
					id, ok := call.Callee.(*ast.Identifier)
					if !ok {
						continue
					}
					switch id.Name {
					case "createSignal":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							out = append(out, Decl{Name: decl.Names[0], Setter: decl.Names[1], Initial: call.Args[0]})
						}
					case "createResource":
						if len(decl.Names) >= 1 && len(call.Args) >= 1 {
							out = append(out, Decl{Name: decl.Names[0], Initial: call.Args[0], IsResource: true})
						}
					case "createCSSChoice":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:    decl.Names[0],
								Setter:  decl.Names[1],
								Initial: call.Args[0],
								CSSKind: CSSKindChoice,
							}
							if len(call.Args) >= 2 {
								d.Options = literalStringOptions(call.Args[1])
							}
							out = append(out, d)
						}
					case "createCSSToggle":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							out = append(out, Decl{
								Name:    decl.Names[0],
								Setter:  decl.Names[1],
								Initial: call.Args[0],
								CSSKind: CSSKindToggle,
								Options: []string{"on"},
							})
						}
					case "createCSSFlags":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							out = append(out, Decl{
								Name:    decl.Names[0],
								Setter:  decl.Names[1],
								Initial: nil,
								CSSKind: CSSKindFlags,
								Options: literalStringOptions(call.Args[0]),
							})
						}
					}
				}
			case *ast.BlockStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.ForStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.WhileStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.DoWhileStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.SwitchStmt:
				if recurse {
					for _, c := range s.Cases {
						walk(c.Body)
					}
				}
			case *ast.TryStmt:
				if recurse {
					walk(s.Body)
					if s.Catch != nil {
						walk(s.Catch.Body)
					}
					walk(s.Finally)
				}
			case *ast.IfStmt:
				if recurse {
					walk(s.Consequent)
					walk(s.Alternate)
				}
			}
		}
	}
	walk(body)
	return out
}

// literalStringOptions extracts the string/number values of a literal array
// expression (createCSSChoice's optional explicit options list). Returns nil
// when the argument is not a literal array.
func literalStringOptions(expr ast.Expr) []string {
	arr, ok := expr.(*ast.ArrayExpr)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range arr.Elements {
		lit, ok := el.(*ast.Literal)
		if !ok {
			return nil
		}
		switch lit.Kind {
		case ast.StringLit, ast.NumberLit:
			out = append(out, lit.Value)
		default:
			return nil
		}
	}
	return out
}
