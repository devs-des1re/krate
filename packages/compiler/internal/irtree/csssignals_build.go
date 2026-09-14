package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/csssignals"
)

// isIntrinsicTag reports whether a JSX tag name is a lowercase HTML element.
func isIntrinsicTag(el *ast.JSXElement) bool {
	if el == nil || el.Opening == nil {
		return false
	}
	name := el.Opening.Name
	return name != "" && name[0] >= 'a' && name[0] <= 'z'
}

// rewriteCSSSignalElement returns a copy of el rewritten for CSS signal
// semantics, or nil when el is neither a trigger nor a panel:
//
//   - trigger: the setter event handler is removed and the element becomes a
//     `<label for>` pointing at the controller input, with the option's trigger
//     class added.
//   - panel: the showIf condition is removed, the panel gains the base class,
//     and it is wrapped in a display:contents element carrying the panel class
//     so the show/hide toggles the wrapper (not the panel's own display).
//
// All rewrites operate on copies; the shared AST is never mutated.
func (b *builder) rewriteCSSSignalElement(el *ast.JSXElement) ast.Expr {
	a := b.cssSignals
	if a == nil {
		return nil
	}

	// Panel first: an element whose showIf matches one of our scopes.
	if panel, ok := a.MatchPanel(el); ok {
		return b.buildCSSPanel(el, panel)
	}

	// Trigger: an element whose handler calls one of our setters.
	if trigger, ok := a.MatchTrigger(el); ok {
		return b.buildCSSTrigger(el, trigger)
	}
	return nil
}

// buildCSSTrigger rewrites a trigger element into a `<label>`.
func (b *builder) buildCSSTrigger(el *ast.JSXElement, t csssignals.Trigger) ast.Expr {
	if !csssignals.CanBeLabel(el.Opening.Name) {
		// Validation rejects this before we get here; keep the element intact
		// as a defensive no-op.
		return nil
	}
	clone := shallowCloneJSX(el)
	clone.Opening.Name = "label"
	clone.Opening.SelfClosing = false
	if clone.Closing != nil {
		clone.Closing.Name = "label"
	}
	var kept []*ast.JSXAttr
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if isOnEvent(attr.Name) || attr.Name == "type" || attr.Name == "href" {
			continue
		}
		kept = append(kept, attr)
	}
	clone.Opening.Attributes = kept
	appendClass(clone, t.Scope.TriggerClass(t.Option))
	clone.Opening.Attributes = append(clone.Opening.Attributes, &ast.JSXAttr{
		Name:  "for",
		Value: &ast.Literal{Kind: ast.StringLit, Value: t.Scope.ControlID(b.cssToken, t.Option)},
	})
	return clone
}

// buildCSSPanel rewrites a panel element: strip the showIf condition, add the
// base panel class, and wrap it in a display:contents wrapper that the generated
// CSS toggles.
func (b *builder) buildCSSPanel(el *ast.JSXElement, p csssignals.Panel) ast.Expr {
	clone := shallowCloneJSX(el)
	clone.Opening.Attributes = filterAttrs(el.Opening.Attributes, func(attr *ast.JSXAttr) bool {
		return attr.Name != "showIf" && attr.Name != "visibleIf"
	})
	// The display:contents wrapper is the toggle target, so the panel keeps its
	// own `display` (`.panel{display:flex}` is preserved when shown). The wrapper
	// must NOT carry an inline style: the stylesheet toggles `display:none` /
	// `display:contents`, and an inline style would beat the stylesheet and keep
	// every panel visible.
	wrapperClass := p.Scope.PanelWrapperClass(p.Option, p.Negated)
	return wrapWithClassOnly([]ast.JSXChild{&ast.JSXElementChild{Element: clone}}, wrapperClass)
}

// wrapWithScopeAnchor builds a `<div class="..." style="display:contents">`
// around children, with optional controller inputs prepended.
func wrapWithScopeAnchor(children []ast.JSXChild, class string, controls []ast.JSXChild) *ast.JSXElement {
	wrapper := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name: "div",
			Attributes: []*ast.JSXAttr{
				strJSXAttr("class", class),
				strJSXAttr("style", "display:contents"),
			},
		},
		Closing: &ast.JSXClosing{Name: "div"},
	}
	if len(controls) > 0 {
		wrapper.Children = append(wrapper.Children, controls...)
	}
	wrapper.Children = append(wrapper.Children, children...)
	return wrapper
}

// wrapWithClassOnly builds a `<div class="...">` around children with no inline
// style, so the generated stylesheet fully controls the wrapper. Used for CSS
// signal panel wrappers, which the stylesheet toggles between `display:none`
// and `display:contents`.
func wrapWithClassOnly(children []ast.JSXChild, class string) *ast.JSXElement {
	wrapper := &ast.JSXElement{
		Opening: &ast.JSXOpening{
			Name:       "div",
			Attributes: []*ast.JSXAttr{strJSXAttr("class", class)},
		},
		Closing: &ast.JSXClosing{Name: "div"},
	}
	wrapper.Children = append(wrapper.Children, children...)
	return wrapper
}

// prepareCSSRoot returns a copy of the component's return expression with the
// scope anchor injected:
//
//   - single intrinsic element root: the scope class is merged onto it and the
//     controller inputs are prepended to its children.
//   - anything else (fragment, component root, dynamic root class): the whole
//     tree is wrapped in a `<div class="krcN" style="display:contents">` anchor.
func (b *builder) prepareCSSRoot(ret ast.Expr, scopes []*csssignals.Scope, token string) ast.Expr {
	if el, ok := ret.(*ast.JSXElement); ok && isIntrinsicTag(el) {
		clone := shallowCloneJSX(el)
		merged := true
		for _, s := range scopes {
			if !appendClass(clone, s.ScopeClass()) {
				merged = false
				break
			}
		}
		if merged {
			clone.Children = append(b.controllerChildren(scopes, token), clone.Children...)
			return clone
		}
	}

	inner := copyExpr(ret)
	var classNames strings.Builder
	for i, s := range scopes {
		if i > 0 {
			classNames.WriteByte(' ')
		}
		classNames.WriteString(s.ScopeClass())
	}
	return wrapWithScopeAnchor(
		[]ast.JSXChild{exprChild(inner)},
		classNames.String(),
		b.controllerChildren(scopes, token),
	)
}

// controllerChildren builds the hidden controller inputs for a component
// instance as JSX children.
func (b *builder) controllerChildren(scopes []*csssignals.Scope, token string) []ast.JSXChild {
	var out []ast.JSXChild
	for _, s := range scopes {
		switch s.Kind {
		case csssignals.KindChoice:
			for _, opt := range s.Options {
				out = append(out, controllerInput(s, token, opt, "radio", opt == s.Initial))
			}
		case csssignals.KindToggle:
			out = append(out, controllerInput(s, token, "on", "checkbox", s.Initial == "true"))
		case csssignals.KindFlags:
			for _, opt := range s.Options {
				out = append(out, controllerInput(s, token, opt, "checkbox", false))
			}
		}
	}
	return out
}

// controllerInput builds a hidden `<input>` controller for one option.
func controllerInput(s *csssignals.Scope, token, opt, inputType string, checked bool) ast.JSXChild {
	class := csssignals.HiddenClass
	if s.Kind == csssignals.KindChoice {
		class += " " + s.RadioClass(opt)
	} else {
		class += " " + s.CheckboxClass(opt)
	}
	attrs := []*ast.JSXAttr{
		strJSXAttr("type", inputType),
		strJSXAttr("class", class),
		strJSXAttr("name", s.ControlName(token, opt)),
		strJSXAttr("id", s.ControlID(token, opt)),
	}
	if s.Kind == csssignals.KindChoice {
		attrs = append(attrs, strJSXAttr("value", opt))
	}
	if checked {
		attrs = append(attrs, &ast.JSXAttr{Name: "checked", Value: &ast.Literal{Kind: ast.BoolLit, Value: "true"}})
	}
	return &ast.JSXElementChild{Element: &ast.JSXElement{
		Opening: &ast.JSXOpening{Name: "input", Attributes: attrs, SelfClosing: true},
	}}
}

// strJSXAttr builds a static string JSX attribute.
func strJSXAttr(name, value string) *ast.JSXAttr {
	return &ast.JSXAttr{Name: name, Value: &ast.Literal{Kind: ast.StringLit, Value: value}}
}

// exprChild wraps an arbitrary expression as a JSX child.
func exprChild(expr ast.Expr) ast.JSXChild {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return &ast.JSXElementChild{Element: e}
	case *ast.JSXFragment:
		return &ast.JSXFragmentChild{Fragment: e}
	default:
		return &ast.JSXExprContainer{Expression: expr}
	}
}

// shallowCloneJSX returns a copy of el's opening/attributes/children containers
// (shallow element copies) so mutation never affects the shared AST.
func shallowCloneJSX(el *ast.JSXElement) *ast.JSXElement {
	opening := *el.Opening
	opening.Attributes = append([]*ast.JSXAttr(nil), el.Opening.Attributes...)
	clone := *el
	clone.Opening = &opening
	if el.Closing != nil {
		closing := *el.Closing
		clone.Closing = &closing
	}
	clone.Children = append([]ast.JSXChild(nil), el.Children...)
	return &clone
}

// copyExpr returns a shallow copy suitable for wrapping.
func copyExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.JSXElement:
		return shallowCloneJSX(e)
	case *ast.JSXFragment:
		clone := *e
		clone.Children = append([]ast.JSXChild(nil), e.Children...)
		return &clone
	}
	return expr
}

// filterAttrs returns the attributes for which keep is true (preserving order).
func filterAttrs(attrs []*ast.JSXAttr, keep func(*ast.JSXAttr) bool) []*ast.JSXAttr {
	var out []*ast.JSXAttr
	for _, a := range attrs {
		if a != nil && keep(a) {
			out = append(out, a)
		}
	}
	return out
}

// appendClass merges cls into a static class/className attribute. Returns false
// when the class attribute is dynamic.
func appendClass(el *ast.JSXElement, cls string) bool {
	for _, attr := range el.Opening.Attributes {
		if attr == nil || (attr.Name != "class" && attr.Name != "className") {
			continue
		}
		if attr.Value == nil {
			attr.Value = &ast.Literal{Kind: ast.StringLit, Value: cls}
			return true
		}
		lit, ok := attr.Value.(*ast.Literal)
		if !ok || lit.Kind != ast.StringLit {
			return false
		}
		if strings.TrimSpace(lit.Value) == "" {
			lit.Value = cls
		} else if !strings.Contains(lit.Value, cls) {
			lit.Value = strings.TrimSpace(lit.Value) + " " + cls
		}
		return true
	}
	el.Opening.Attributes = append(el.Opening.Attributes, &ast.JSXAttr{
		Name:  "class",
		Value: &ast.Literal{Kind: ast.StringLit, Value: cls},
	})
	return true
}
