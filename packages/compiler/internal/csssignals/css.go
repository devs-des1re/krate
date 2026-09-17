package csssignals

import (
	"sort"
	"strings"
)

// activeTriggerDecl is the declaration block applied to a selected trigger.
// Themes override it via the --krate-css-active* custom properties.
const activeTriggerDecl = "color:var(--krate-css-active-fg,inherit);border-bottom:2px solid var(--krate-css-active,#3b82f6);font-weight:600"

// HiddenCSS is the base rule for the visually-hidden controller inputs. They
// stay focusable (unlike display:none) so keyboard navigation and screen readers
// keep working.
const HiddenCSS = "." + HiddenClass + "{position:absolute;width:1px;height:1px;margin:-1px;padding:0;border:0;clip:rect(0 0 0 0);overflow:hidden;white-space:nowrap}"

// Stylesheet returns the generated stylesheet for scopes and their matched
// conditions, in a deterministic order so the page's content hash is stable.
// Returns "" when empty. Conditions whose wrapper is hidden/shown by the
// per-scope rules (single positive choice atom, any single toggle/flag atom)
// are canonical and are not double-emitted.
func Stylesheet(scopes []*Scope, conditions []*Condition) string {
	if len(scopes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(HiddenCSS)
	b.WriteByte('\n')
	for _, s := range scopes {
		b.WriteString(s.css())
	}
	for _, c := range sortedConditions(conditions) {
		if c.needsExplicitRule() {
			b.WriteString(c.css())
		}
	}
	return b.String()
}

// sortedConditions returns a deterministic copy ordered by owning scope index
// then expression index.
func sortedConditions(conditions []*Condition) []*Condition {
	out := append([]*Condition(nil), conditions...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].owningScope().Index, out[j].owningScope().Index; a != b {
			return a < b
		}
		return out[i].ExprIdx < out[j].ExprIdx
	})
	return out
}

// needsExplicitRule reports whether the condition needs its own hide+show rule
// beyond the per-scope choice/toggle/flags rules. A positive choice atom, a
// toggle, and a flag are canonical (covered by their scope's css); a negated
// choice atom and every compound condition are not.
func (c *Condition) needsExplicitRule() bool {
	if !c.simple() {
		return true
	}
	at := c.Terms[0][0]
	return at.Scope.Kind == KindChoice && at.Negated
}

// css emits the hide + show rules for a non-canonical condition. The wrapper is
// hidden by default, then shown when any DNF term matches: one selector-list
// entry per AND-term.
func (c *Condition) css() string {
	owner := c.owningScope()
	var b strings.Builder
	b.WriteString("." + owner.Class + " ." + c.Class + "{display:none}\n")
	for i, term := range c.Terms {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + owner.Class)
		for _, at := range term {
			b.WriteString(at.selector())
		}
		b.WriteString(" ." + c.Class)
	}
	b.WriteString("{display:contents}\n")
	return b.String()
}

// selector is the :has()/:not(:has()) fragment for an atom, relative to the
// scope anchor element.
func (at *Atom) selector() string {
	if at.Negated {
		return ":not(:has(" + at.controllerSel() + ":checked))"
	}
	return ":has(" + at.controllerSel() + ":checked)"
}

// controllerSel is the class selector of the atom's controller input.
func (at *Atom) controllerSel() string {
	if at.Scope.Kind == KindChoice {
		return "." + at.Scope.RadioClass(at.Option)
	}
	return "." + at.Scope.CheckboxClass(at.Option)
}

// css emits the rules for a single scope.
func (s *Scope) css() string {
	switch s.Kind {
	case KindChoice:
		return s.choiceCSS()
	case KindToggle:
		return s.toggleCSS()
	case KindFlags:
		return s.flagsCSS()
	}
	return ""
}

// PanelWrapperClass is the class on the display:contents wrapper around a panel.
// The wrapper is what gets toggled, so the panel's own author `display` is
// preserved. `negated` selects the "state is off" wrapper.
func (s *Scope) PanelWrapperClass(option string, negated bool) string {
	if negated {
		return s.Class + "-n-" + sanitizeToken(option)
	}
	return s.Class + "-p-" + sanitizeToken(option)
}

// choiceCSS emits the rules for a radio group.
func (s *Scope) choiceCSS() string {
	var b strings.Builder

	// Every option's panel wrapper is hidden by default; the selected option's
	// wrapper becomes layout-transparent. The panel itself keeps its display.
	for i, opt := range s.Options {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + s.Class + " ." + s.PanelWrapperClass(opt, false))
	}
	b.WriteString("{display:none}\n")

	for i, opt := range s.Options {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + s.PanelWrapperClass(opt, false))
	}
	b.WriteString("{display:contents}\n")

	for _, opt := range s.Options {
		b.WriteString("." + s.Class + ":has(." + s.RadioClass(opt) + ":checked) ." + s.TriggerClass(opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	return b.String()
}

// toggleCSS emits the rules for a single checkbox. Two panel wrappers may be
// used: the "on" wrapper (shown when checked) and the "off" wrapper (shown when
// not) — either or both may be absent in the markup.
func (s *Scope) toggleCSS() string {
	var b strings.Builder
	cb := s.CheckboxClass("")
	on, off := s.PanelWrapperClass("on", false), s.PanelWrapperClass("on", true)

	b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
	b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + on)
	b.WriteString(",." + s.Class + ":not(:has(." + cb + ":checked)) ." + off)
	b.WriteString("{display:contents}\n")
	b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + s.TriggerClass(""))
	b.WriteString("{" + activeTriggerDecl + "}\n")
	return b.String()
}

// flagsCSS emits the rules for independent checkboxes.
func (s *Scope) flagsCSS() string {
	var b strings.Builder
	for _, opt := range s.Options {
		on, off := s.PanelWrapperClass(opt, false), s.PanelWrapperClass(opt, true)
		cb := s.CheckboxClass(opt)
		b.WriteString("." + s.Class + " ." + on + ",." + s.Class + " ." + off + "{display:none}\n")
		b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + on)
		b.WriteString(",." + s.Class + ":not(:has(." + cb + ":checked)) ." + off)
		b.WriteString("{display:contents}\n")
		b.WriteString("." + s.Class + ":has(." + cb + ":checked) ." + s.TriggerClass(opt))
		b.WriteString("{" + activeTriggerDecl + "}\n")
	}
	return b.String()
}
