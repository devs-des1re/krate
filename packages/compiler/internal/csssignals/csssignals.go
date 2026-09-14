// Package csssignals implements the compiler-side analysis for the zero-JS CSS
// primitives:
//
//   - createCSSChoice(initial, options?) — a radio group (tabs, segments).
//   - createCSSToggle(initial)           — a single checkbox (on/off).
//   - createCSSFlags([...])              — independent checkboxes.
//
// They compile to hidden `<input>` controllers, `<label>` triggers, and
// `:has()` CSS — no client JavaScript. This package owns the analysis (which
// declarations are transformable, the option universe, matching
// triggers/panels) and the generated stylesheet. It never mutates the AST.
//
// Unlike a fallback design, an un-transformable declaration is a hard error: a
// declaration that cannot be expressed in CSS must be replaced with
// `createSignal` by the author, because silently hydrating it would ship
// behaviour the author did not ask for.
package csssignals

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/sigutil"
)

// Kind identifies the CSS signal primitive.
type Kind int

const (
	// KindChoice is a radio group (`createCSSChoice`).
	KindChoice Kind = iota
	// KindToggle is a single checkbox (`createCSSToggle`).
	KindToggle
	// KindFlags is a set of independent checkboxes (`createCSSFlags`).
	KindFlags
)

// ClassPrefix is the shared prefix for every generated class/identifier. Kept
// short deliberately: these tokens ship to the browser on every page.
const ClassPrefix = "krc"

// HiddenClass marks the visually-hidden controller inputs.
const HiddenClass = ClassPrefix + "-h"

// Scope is one CSS signal declaration within a component. It is shared by every
// instance of that component: all selectors are class-based and
// instance-agnostic. Per-instance input `name`/`id` uniqueness is handled by the
// builder via an instance token.
type Scope struct {
	Kind Kind
	// Var is the getter identifier: the choice/toggle getter, or the flags
	// object for KindFlags.
	Var string
	// Setter is the setter identifier.
	Setter string
	// Initial is the initial selection for choice/toggle ("" for flags).
	Initial string
	// Options is the ordered option universe: choice values, ["on"] for a
	// toggle, or flag names for flags.
	Options []string
	// Index is the page-unique scope index; the root class is ClassPrefix+index.
	Index int
	// Class is the scope's root class, shared by every instance.
	Class string
}

// base returns the scope's root class (e.g. "krc0").
func (s *Scope) base() string { return ClassPrefix + encodeIndex(s.Index) }

// RadioClass returns the class marking the controller input for a choice
// option.
func (s *Scope) RadioClass(option string) string {
	return s.Class + "-r-" + sanitizeToken(option)
}

// CheckboxClass returns the class marking the checkbox controller for a toggle
// (no option) or a flag option.
func (s *Scope) CheckboxClass(option string) string {
	if s.Kind == KindToggle {
		return s.Class + "-c"
	}
	return s.Class + "-c-" + sanitizeToken(option)
}

// TriggerClass returns the class applied to a trigger label for an option
// (choice/flags) or the sole toggle trigger.
func (s *Scope) TriggerClass(option string) string {
	if s.Kind == KindToggle {
		return s.Class + "-t"
	}
	return s.Class + "-t-" + sanitizeToken(option)
}

// ScopeClass is the class the builder adds to the scope anchor.
func (s *Scope) ScopeClass() string { return s.Class }

// ControlName returns the controller input's `name` for one instance. Choice
// uses a shared name (radio group); toggle/flags are independent checkboxes and
// so get a per-option name.
func (s *Scope) ControlName(token, option string) string {
	switch s.Kind {
	case KindChoice:
		return token + "-r" + encodeIndex(s.Index)
	default:
		return token + "-c" + encodeIndex(s.Index) + "-" + sanitizeToken(option)
	}
}

// ControlID returns the controller input's DOM id for one instance/option. Only
// a radio group needs the option appended (its name is shared); toggle/flag
// names are already unique.
func (s *Scope) ControlID(token, option string) string {
	if s.Kind == KindChoice {
		return s.ControlName(token, option) + "-" + sanitizeToken(option)
	}
	return s.ControlName(token, option)
}

// Trigger is a matched setter call.
type Trigger struct {
	Scope  *Scope
	Option string // choice/flags option; "on" for toggle
}

// Panel is a matched showIf condition.
type Panel struct {
	Scope   *Scope
	Option  string
	Negated bool // true for `!get()` / `flags.x()` false-branch panels
}

// Analyzer holds the CSS signal scopes for a single component plus the
// validation result. A component with any CSS signal that cannot be compiled
// yields errors and must fail the build.
type Analyzer struct {
	scopes   []*Scope
	bySetter map[string]*Scope
	byVar    map[string]*Scope
	haveAny  bool
	errs     []string
}

// indexFor resolves a stable page-unique index for a (component, var) pair.
type indexFor func(component, variable string) int

// Analyze inspects a component body for CSS signal declarations.
//
// When no declaration is present, HaveAny reports false and Errors is empty.
// When a declaration is present but not compilable, Errors lists the reasons
// (the build must fail; there is no fallback).
func Analyze(component string, body []ast.Stmt, index indexFor) *Analyzer {
	a := &Analyzer{
		bySetter: make(map[string]*Scope),
		byVar:    make(map[string]*Scope),
	}
	var decls []sigutil.Decl
	for _, d := range sigutil.Find(body, true) {
		if d.CSSKind != sigutil.CSSKindNone {
			decls = append(decls, d)
		}
	}
	if len(decls) == 0 {
		return a
	}
	a.haveAny = true

	for _, d := range decls {
		switch d.CSSKind {
		case sigutil.CSSKindChoice:
			if d.Initial == nil || d.Setter == "" {
				a.errs = append(a.errs, "createCSSChoice needs a literal initial value and a setter")
				continue
			}
			initial, ok := literalValue(d.Initial)
			if !ok {
				a.errs = append(a.errs, "createCSSChoice's initial value must be a string or number literal")
				continue
			}
			options := d.Options
			if len(options) == 0 {
				options = inferOptions(body, d.Setter, initial)
			} else if !contains(options, initial) {
				options = append([]string{initial}, options...)
			}
			a.addScope(&Scope{Kind: KindChoice, Var: d.Name, Setter: d.Setter, Initial: initial, Options: options})
		case sigutil.CSSKindToggle:
			if d.Initial == nil || d.Setter == "" {
				a.errs = append(a.errs, "createCSSToggle needs a literal initial value and a setter")
				continue
			}
			initial, ok := literalValue(d.Initial)
			if !ok {
				a.errs = append(a.errs, "createCSSToggle's initial value must be true or false")
				continue
			}
			if initial != "true" && initial != "false" {
				a.errs = append(a.errs, "createCSSToggle's initial value must be true or false")
				continue
			}
			a.addScope(&Scope{Kind: KindToggle, Var: d.Name, Setter: d.Setter, Initial: initial, Options: []string{"on"}})
		case sigutil.CSSKindFlags:
			if d.Setter == "" || len(d.Options) == 0 {
				a.errs = append(a.errs, "createCSSFlags needs a literal array of flag names and a setter")
				continue
			}
			a.addScope(&Scope{Kind: KindFlags, Var: d.Name, Setter: d.Setter, Options: d.Options})
		}
	}

	if len(a.errs) == 0 {
		if r := a.validate(body); r != "" {
			a.errs = append(a.errs, r)
		}
	}
	if len(a.errs) > 0 {
		return a
	}

	// Assign stable indices/classes now that we know the component compiles.
	for _, s := range a.scopes {
		s.Index = index(component, s.Var)
		s.Class = s.base()
	}
	return a
}

func (a *Analyzer) addScope(s *Scope) {
	a.scopes = append(a.scopes, s)
	a.bySetter[s.Setter] = s
	a.byVar[s.Var] = s
}

// HaveAny reports whether the component declares any CSS signal.
func (a *Analyzer) HaveAny() bool { return a.haveAny }

// OK reports whether the component compiles with no errors.
func (a *Analyzer) OK() bool { return a.haveAny && len(a.errs) == 0 }

// Errors returns the reasons a CSS signal could not be compiled.
func (a *Analyzer) Errors() []string { return a.errs }

// Scopes returns the analyzed scopes in declaration order. Only meaningful when
// OK is true.
func (a *Analyzer) Scopes() []*Scope { return a.scopes }

// names returns every getter/setter/var identifier belonging to a scope.
func (a *Analyzer) names() map[string]bool {
	set := make(map[string]bool, len(a.scopes)*2)
	for _, s := range a.scopes {
		set[s.Var] = true
		set[s.Setter] = true
	}
	return set
}

// MatchTrigger matches an element whose event handler is a valid setter call
// for one of this component's scopes.
func (a *Analyzer) MatchTrigger(el *ast.JSXElement) (Trigger, bool) {
	if el == nil || el.Opening == nil {
		return Trigger{}, false
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil || !isOnEvent(attr.Name) {
			continue
		}
		if t, ok := a.matchHandler(attr.Value); ok {
			return t, true
		}
	}
	return Trigger{}, false
}

// matchHandler matches an arrow whose entire body is one setter call.
func (a *Analyzer) matchHandler(handler ast.Expr) (Trigger, bool) {
	fn, ok := handler.(*ast.ArrowFn)
	if !ok || len(fn.Body) != 1 {
		return Trigger{}, false
	}
	var expr ast.Expr
	switch b := fn.Body[0].(type) {
	case *ast.ExprStmt:
		expr = b.Expression
	case *ast.ReturnStmt:
		expr = b.Value
	default:
		return Trigger{}, false
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return Trigger{}, false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return Trigger{}, false
	}
	s := a.bySetter[id.Name]
	if s == nil {
		return Trigger{}, false
	}
	return a.matchSetterArgs(s, call)
}

// matchSetterArgs validates the setter call shape per kind.
func (a *Analyzer) matchSetterArgs(s *Scope, call *ast.CallExpr) (Trigger, bool) {
	switch s.Kind {
	case KindChoice:
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		if v, ok := literalValue(call.Args[0]); ok && contains(s.Options, v) {
			return Trigger{Scope: s, Option: v}, true
		}
	case KindToggle:
		if len(call.Args) != 1 {
			return Trigger{}, false
		}
		// `set(!on())` (toggle) or `set(true)`/`set(false)`. A label only
		// toggles, so any accepted form maps to "click toggles the checkbox".
		arg := call.Args[0]
		if isNotGetterCall(arg, s.Var) {
			return Trigger{Scope: s, Option: "on"}, true
		}
		if _, ok := literalBool(arg); ok {
			return Trigger{Scope: s, Option: "on"}, true
		}
	case KindFlags:
		if len(call.Args) != 2 {
			return Trigger{}, false
		}
		name, ok := literalValue(call.Args[0])
		if !ok || !contains(s.Options, name) {
			return Trigger{}, false
		}
		// The value may be a literal bool or `!flags.x()`. A label only
		// toggles the checkbox, so the option name is what matters.
		if _, ok := literalBool(call.Args[1]); !ok && !isNotFlagCall(call.Args[1]) {
			return Trigger{}, false
		}
		return Trigger{Scope: s, Option: name}, true
	}
	return Trigger{}, false
}

// MatchPanel matches an element whose `showIf`/`visibleIf` test selects one of
// this component's scopes.
func (a *Analyzer) MatchPanel(el *ast.JSXElement) (Panel, bool) {
	if el == nil || el.Opening == nil {
		return Panel{}, false
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			continue
		}
		if attr.Name != "showIf" && attr.Name != "visibleIf" {
			continue
		}
		if p, ok := a.matchPanelTest(attr.Value); ok {
			return p, true
		}
	}
	return Panel{}, false
}

func (a *Analyzer) matchPanelTest(test ast.Expr) (Panel, bool) {
	// Negation: `!expr` (toggle/flag false-branch).
	if u, ok := test.(*ast.UnaryExpr); ok && u.Op == "!" {
		if p, ok := a.matchTruthy(u.Arg); ok {
			p.Negated = true
			return p, true
		}
	}
	// Comparison: `get() === literal` (choice).
	if bin, ok := test.(*ast.BinaryExpr); ok && (bin.Op == "===" || bin.Op == "==") {
		if p, ok := a.matchCompare(bin.Left, bin.Right); ok {
			return p, true
		}
		if p, ok := a.matchCompare(bin.Right, bin.Left); ok {
			return p, true
		}
	}
	// Truthiness: `get()` (toggle) or `flags.x()` (flag).
	if p, ok := a.matchTruthy(test); ok {
		return p, true
	}
	return Panel{}, false
}

// matchCompare matches `get() === literal` for a choice scope.
func (a *Analyzer) matchCompare(getterSide, literalSide ast.Expr) (Panel, bool) {
	call, ok := getterSide.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return Panel{}, false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return Panel{}, false
	}
	s := a.byVar[id.Name]
	if s == nil || s.Kind != KindChoice {
		return Panel{}, false
	}
	val, ok := literalValue(literalSide)
	if !ok || !contains(s.Options, val) {
		return Panel{}, false
	}
	return Panel{Scope: s, Option: val}, true
}

// matchTruthy matches a bare truthy getter read: `on()` (toggle) or
// `flags.x()` (flag).
func (a *Analyzer) matchTruthy(expr ast.Expr) (Panel, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return Panel{}, false
	}
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		s := a.byVar[callee.Name]
		if s == nil || s.Kind != KindToggle {
			return Panel{}, false
		}
		return Panel{Scope: s, Option: "on"}, true
	case *ast.MemberExpr:
		obj, ok := callee.Object.(*ast.Identifier)
		if !ok {
			return Panel{}, false
		}
		s := a.byVar[obj.Name]
		if s == nil || s.Kind != KindFlags {
			return Panel{}, false
		}
		prop, ok := callee.Property.(*ast.Identifier)
		if !ok || !contains(s.Options, prop.Name) {
			return Panel{}, false
		}
		return Panel{Scope: s, Option: prop.Name}, true
	}
	return Panel{}, false
}

// literalBool resolves a boolean literal.
func literalBool(expr ast.Expr) (bool, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok || lit.Kind != ast.BoolLit {
		return false, false
	}
	return lit.Value == "true", true
}

// isNotFlagCall reports whether expr is `!flags.<x>()`.
func isNotFlagCall(expr ast.Expr) bool {
	u, ok := expr.(*ast.UnaryExpr)
	if !ok || u.Op != "!" {
		return false
	}
	call, ok := u.Arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	_, ok = call.Callee.(*ast.MemberExpr)
	return ok
}

// isNotGetterCall reports whether expr is `!getter()` for the given getter.
func isNotGetterCall(expr ast.Expr, getter string) bool {
	u, ok := expr.(*ast.UnaryExpr)
	if !ok || u.Op != "!" {
		return false
	}
	call, ok := u.Arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	id, ok := call.Callee.(*ast.Identifier)
	return ok && id.Name == getter
}

// literalValue resolves a string/number/bool literal to its string value.
func literalValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return "", false
	}
	switch lit.Kind {
	case ast.StringLit, ast.NumberLit, ast.BoolLit:
		return lit.Value, true
	}
	return "", false
}

// inferOptions collects every literal the choice setter is invoked with
// throughout the body (in source order), then prepends the initial value.
func inferOptions(body []ast.Stmt, setter, initial string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(v string) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(initial)
	walkStmts(body, func(call *ast.CallExpr) {
		id, ok := call.Callee.(*ast.Identifier)
		if !ok || id.Name != setter || len(call.Args) != 1 {
			return
		}
		if v, ok := literalValue(call.Args[0]); ok {
			add(v)
		}
	})
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// sanitizeToken converts an arbitrary option token to a CSS-safe suffix.
func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	t := strings.Trim(b.String(), "-")
	if t == "" {
		return "x"
	}
	return t
}

// SanitizeToken is the exported form used by the IR builder for instance tokens.
func SanitizeToken(s string) string { return sanitizeToken(s) }

const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// encodeIndex encodes a scope index in base62 for compact class names.
func encodeIndex(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Chars[n%62]
		n /= 62
	}
	return string(buf[i:])
}
