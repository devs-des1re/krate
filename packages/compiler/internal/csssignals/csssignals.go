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
	"sort"
	"strconv"
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

// Atom is one state predicate: a choice `get()==='x'` (or `!==`), a toggle
// `on()` (or `!on()`), or a flag `flags.x()` (or `!flags.x()`).
type Atom struct {
	Scope   *Scope
	Option  string
	Negated bool // literal polarity: `get()!=='x'`, `!on()`, `!flags.x()`
}

// Condition is a panel's boolean test normalized to DNF: an OR of AND-terms,
// each term a set of literals (atoms or their negation). Two conditions with
// the same canonical DNF are interchangeable and dedupe to one wrapper class.
type Condition struct {
	Scopes  []*Scope  // distinct scopes referenced, first-appearance order
	Terms   [][]*Atom // DNF: OR of AND-terms; each term is a sorted atom set
	Class   string    // wrapper class: krcN-p-<x>/krcN-n-<x> (simple) or krcN-x-<i> (compound)
	ExprIdx int       // component-local index for compound wrapper classes
}

// Panel is a matched showIf/visibleIf condition.
type Panel struct {
	Cond *Condition
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

	// conditions holds every deduped matched panel condition (registration
	// order), condByKey maps a canonical DNF string to its condition, and
	// exprIdx is the component-local counter for compound wrapper classes.
	conditions []*Condition
	condByKey  map[string]*Condition
	exprIdx    int
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

	// Register every matched panel condition (deduped) in source order now that
	// scope classes exist, so Conditions() is populated for callers and the
	// builder can emit compound show rules.
	if ret := findReturnStmt(body); ret != nil {
		a.registerPanels(ret.Value)
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
// this component's scopes. Matching a panel registers its condition (assigning
// a deterministic wrapper class); identical conditions dedupe to one class.
func (a *Analyzer) MatchPanel(el *ast.JSXElement) (Panel, bool) {
	cond, ok := a.ParsePanel(el)
	if !ok {
		return Panel{}, false
	}
	return Panel{Cond: a.registerCondition(cond)}, true
}

// ParsePanel parses an element's `showIf`/`visibleIf` test into a condition
// without registering it. Validation uses this so it can classify a panel
// without consuming wrapper-class indices.
func (a *Analyzer) ParsePanel(el *ast.JSXElement) (*Condition, bool) {
	if el == nil || el.Opening == nil {
		return nil, false
	}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Value == nil {
			continue
		}
		if attr.Name != "showIf" && attr.Name != "visibleIf" {
			continue
		}
		return a.matchPanelTest(attr.Value)
	}
	return nil, false
}

// Conditions returns the registered conditions in dedupe (first-match) order.
func (a *Analyzer) Conditions() []*Condition { return a.conditions }

// registerCondition returns the canonical condition for c, assigning its
// wrapper class. Identical conditions dedupe to the same pointer and class; a
// condition with a single atom keeps the classic per-scope wrapper class, while
// compound conditions consume a component-local ExprIdx.
func (a *Analyzer) registerCondition(c *Condition) *Condition {
	if a.condByKey == nil {
		a.condByKey = make(map[string]*Condition)
	}
	key := a.conditionKey(c)
	if existing, ok := a.condByKey[key]; ok {
		return existing
	}
	if c.simple() {
		at := c.Terms[0][0]
		c.Class = at.Scope.PanelWrapperClass(at.Option, at.Negated)
	} else {
		c.ExprIdx = a.exprIdx
		a.exprIdx++
		c.Class = c.owningScope().Class + "-x-" + strconv.Itoa(c.ExprIdx)
	}
	a.condByKey[key] = c
	a.conditions = append(a.conditions, c)
	return c
}

// registerPanels walks the component's returned JSX and registers every matched
// panel condition (deduped, in source order). Called by Analyze after scope
// classes are stamped so Conditions() is populated without the builder.
func (a *Analyzer) registerPanels(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e == nil {
			return
		}
		if _, ok := a.MatchPanel(e); ok {
			// Registered (or deduped to an existing condition).
		}
		for _, child := range e.Children {
			a.registerPanelChild(child)
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			a.registerPanelChild(child)
		}
	case *ast.TypeAssertion:
		a.registerPanels(e.Expr)
	}
}

func (a *Analyzer) registerPanelChild(child ast.JSXChild) {
	switch c := child.(type) {
	case *ast.JSXElementChild:
		a.registerPanels(c.Element)
	case *ast.JSXFragmentChild:
		a.registerPanels(c.Fragment)
	case *ast.JSXExprContainer:
		a.registerPanels(c.Expression)
	}
}

// matchPanelTest parses a test expression into a DNF condition over this
// component's atoms. An expression that is not classifiable returns ok=false.
func (a *Analyzer) matchPanelTest(test ast.Expr) (*Condition, bool) {
	terms, ok := a.parseBoolean(test)
	if !ok {
		return nil, false
	}
	terms = a.normalizeTerms(terms)
	if len(terms) == 0 {
		return nil, false
	}
	return &Condition{Terms: terms, Scopes: collectScopes(terms)}, true
}

// parseBoolean recursively parses a boolean expression into DNF terms: OR of
// (AND of atoms). Parens are transparent in this parser's AST, so no grouped
// expression node needs special handling.
func (a *Analyzer) parseBoolean(expr ast.Expr) ([][]*Atom, bool) {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op != "!" {
			return nil, false
		}
		terms, ok := a.parseBoolean(e.Arg)
		if !ok {
			return nil, false
		}
		return a.negate(terms), true
	case *ast.BinaryExpr:
		switch e.Op {
		case "&&":
			l, lok := a.parseBoolean(e.Left)
			if !lok {
				return nil, false
			}
			r, rok := a.parseBoolean(e.Right)
			if !rok {
				return nil, false
			}
			return a.and(l, r), true
		case "||":
			l, lok := a.parseBoolean(e.Left)
			if !lok {
				return nil, false
			}
			r, rok := a.parseBoolean(e.Right)
			if !rok {
				return nil, false
			}
			return append(l, r...), true
		case "===", "==", "!==", "!=":
			negated := e.Op == "!==" || e.Op == "!="
			if at, ok := a.matchCompareAtom(e.Left, e.Right, negated); ok {
				return [][]*Atom{{at}}, true
			}
			if at, ok := a.matchCompareAtom(e.Right, e.Left, negated); ok {
				return [][]*Atom{{at}}, true
			}
			return nil, false
		}
		return nil, false
	case *ast.CallExpr:
		if at, ok := a.matchTruthyAtom(e); ok {
			return [][]*Atom{{at}}, true
		}
	}
	return nil, false
}

// matchCompareAtom matches `get() === literal` for a choice scope, or its
// negated form when negated is true (`get() !== literal`).
func (a *Analyzer) matchCompareAtom(getterSide, literalSide ast.Expr, negated bool) (*Atom, bool) {
	call, ok := getterSide.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return nil, false
	}
	s := a.byVar[id.Name]
	if s == nil || s.Kind != KindChoice {
		return nil, false
	}
	val, ok := literalValue(literalSide)
	if !ok || !contains(s.Options, val) {
		return nil, false
	}
	return &Atom{Scope: s, Option: val, Negated: negated}, true
}

// matchTruthyAtom matches a bare truthy getter read: `on()` (toggle) or
// `flags.x()` (flag).
func (a *Analyzer) matchTruthyAtom(expr ast.Expr) (*Atom, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		s := a.byVar[callee.Name]
		if s == nil || s.Kind != KindToggle {
			return nil, false
		}
		return &Atom{Scope: s, Option: "on"}, true
	case *ast.MemberExpr:
		obj, ok := callee.Object.(*ast.Identifier)
		if !ok {
			return nil, false
		}
		s := a.byVar[obj.Name]
		if s == nil || s.Kind != KindFlags {
			return nil, false
		}
		prop, ok := callee.Property.(*ast.Identifier)
		if !ok || !contains(s.Options, prop.Name) {
			return nil, false
		}
		return &Atom{Scope: s, Option: prop.Name}, true
	}
	return nil, false
}

// and conjoins two DNF term lists via the cross product, pruning terms that
// become a contradiction.
func (a *Analyzer) and(x, y [][]*Atom) [][]*Atom {
	out := make([][]*Atom, 0, len(x)*len(y))
	for _, tx := range x {
		for _, ty := range y {
			merged := make([]*Atom, 0, len(tx)+len(ty))
			merged = append(merged, tx...)
			merged = append(merged, ty...)
			if term := a.mergeTerm(merged); len(term) > 0 {
				out = append(out, term)
			}
		}
	}
	return out
}

// negate returns the DNF of `!expr` given the DNF of `expr`, applying De
// Morgan: each AND-term's literals negate to a clause (OR of negated atoms),
// and the clauses are ANDed via the cross product.
func (a *Analyzer) negate(terms [][]*Atom) [][]*Atom {
	result := [][]*Atom{{}}
	for _, t := range terms {
		var next [][]*Atom
		for _, lit := range t {
			neg := *lit
			neg.Negated = !neg.Negated
			for _, acc := range result {
				cand := make([]*Atom, 0, len(acc)+1)
				cand = append(cand, acc...)
				cand = append(cand, &neg)
				if m := a.mergeTerm(cand); len(m) > 0 {
					next = append(next, m)
				}
			}
		}
		if len(next) == 0 {
			return nil // contradiction
		}
		result = next
	}
	return result
}

// mergeTerm dedupes atoms within one AND-term and drops the term when it
// contains an atom and its negation (an impossible conjunction).
func (a *Analyzer) mergeTerm(atoms []*Atom) []*Atom {
	saw := make(map[string]*Atom, len(atoms)) // abs-key → first atom (records polarity)
	out := make([]*Atom, 0, len(atoms))
	for _, at := range atoms {
		abs := a.atomAbsKey(at)
		if prev, ok := saw[abs]; ok {
			if prev.Negated != at.Negated {
				return nil // opposite literals in one AND-term
			}
			continue // duplicate literal
		}
		saw[abs] = at
		out = append(out, at)
	}
	return out
}

// normalizeTerms canonicalizes a DNF: sorts atoms within each term and the
// terms themselves, and dedupes duplicate terms.
func (a *Analyzer) normalizeTerms(terms [][]*Atom) [][]*Atom {
	seen := make(map[string]bool, len(terms))
	out := make([][]*Atom, 0, len(terms))
	for _, t := range terms {
		t = a.mergeTerm(t)
		if len(t) == 0 {
			continue
		}
		sort.Slice(t, func(i, j int) bool { return a.atomKey(t[i]) < a.atomKey(t[j]) })
		key := a.termKey(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return a.termKey(out[i]) < a.termKey(out[j]) })
	return out
}

// conditionKey is the canonical dedupe key for a condition: the sorted term
// keys joined by ";".
func (a *Analyzer) conditionKey(c *Condition) string {
	var b strings.Builder
	for i, t := range c.Terms {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(a.termKey(t))
	}
	return b.String()
}

func (a *Analyzer) termKey(t []*Atom) string {
	var b strings.Builder
	for i, at := range t {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(a.atomKey(at))
	}
	return b.String()
}

// atomKey is the deterministic total-order key for sorting/deduping atoms. It
// uses the scope's declaration position (stable across analysis phases), not
// its page index, so dedupe is phase-independent.
func (a *Analyzer) atomKey(at *Atom) string {
	return atomKeyAt(a.scopePos(at.Scope), at.Option, at.Negated)
}

// atomAbsKey identifies an atom scope+option regardless of polarity, for
// contradiction detection within a term.
func (a *Analyzer) atomAbsKey(at *Atom) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(a.scopePos(at.Scope)))
	b.WriteByte('|')
	b.WriteString(at.Option)
	return b.String()
}

func atomKeyAt(pos int, option string, negated bool) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(pos))
	b.WriteByte('|')
	b.WriteString(option)
	b.WriteByte('|')
	if negated {
		b.WriteByte('1')
	} else {
		b.WriteByte('0')
	}
	return b.String()
}

// scopePos returns the declaration position of a scope in the analyzer.
func (a *Analyzer) scopePos(s *Scope) int {
	for i, x := range a.scopes {
		if x == s {
			return i
		}
	}
	return -1
}

// simple reports whether the condition is a single atom, which keeps the
// classic per-scope wrapper classes (krcN-p-<x> / krcN-n-<x>).
func (c *Condition) simple() bool {
	return len(c.Terms) == 1 && len(c.Terms[0]) == 1
}

// owningScope is the anchor scope: the referenced scope with the lowest page
// index. Its class prefixes the selectors and the compound wrapper class.
func (c *Condition) owningScope() *Scope {
	var owner *Scope
	for _, s := range c.Scopes {
		if owner == nil || s.Index < owner.Index {
			owner = s
		}
	}
	return owner
}

// OwningScope returns the anchor scope (the referenced scope with the lowest
// page index), used by the builder to order deduped conditions deterministically.
func (c *Condition) OwningScope() *Scope { return c.owningScope() }

// collectScopes returns the distinct scopes referenced by a DNF term list, in
// first-appearance order.
func collectScopes(terms [][]*Atom) []*Scope {
	var out []*Scope
	seen := make(map[*Scope]bool)
	for _, t := range terms {
		for _, at := range t {
			if !seen[at.Scope] {
				seen[at.Scope] = true
				out = append(out, at.Scope)
			}
		}
	}
	return out
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
