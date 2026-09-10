package css

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

var classSelector = regexp.MustCompile(`\.([a-zA-Z_][a-zA-Z0-9_-]*)`)

type Asset struct {
	Path     string
	Content  string
	IsModule bool
}

type ModuleMapping struct {
	LocalVar string
	Mappings map[string]string
}

func Collect(dir string) ([]*Asset, error) {
	var assets []*Asset

	err := fsutil.WalkExt(dir, map[string]bool{".css": true}, nil, func(path string, info os.FileInfo) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		isModule := strings.Contains(info.Name(), ".module.")

		assets = append(assets, &Asset{
			Path:     path,
			Content:  string(data),
			IsModule: isModule,
		})
		return nil
	})

	return assets, err
}

func ProcessModule(path, content string) (scopedCSS string, mapping map[string]string, err error) {
	hash := hashPath(path)
	seen := make(map[string]bool)
	mapping = make(map[string]string)

	scoped := classSelector.ReplaceAllStringFunc(content, func(match string) string {
		name := match[1:]
		if seen[name] {
			return "." + mapping[name]
		}
		seen[name] = true
		scoped := name + "_" + hash
		mapping[name] = scoped
		return "." + scoped
	})

	return scoped, mapping, nil
}

func hashPath(path string) string {
	h := fnv.New32a()
	h.Write([]byte(path))
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}

func Minify(css string) string {
	var b strings.Builder
	b.Grow(len(css))
	i := 0
	inBlock := false
	wasSpace := false
	n := len(css)

	for i < n {
		ch := css[i]
		switch {
		case ch == '/' && i+1 < n && css[i+1] == '*':
			inBlock = true
			i += 2
		case ch == '*' && i+1 < n && css[i+1] == '/':
			inBlock = false
			i += 2
		case inBlock:
			i++
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if !wasSpace {
				b.WriteByte(' ')
				wasSpace = true
			}
			i++
		case ch == '{' || ch == '}' || ch == ';' || ch == ',':
			b.WriteByte(ch)
			wasSpace = false
			i++
		default:
			b.WriteByte(ch)
			wasSpace = false
			i++
		}
	}

	minified := strings.TrimSpace(b.String())

	minified = shortenHexColors(minified)
	minified = rgbaToHex(minified)
	minified = removeZeroUnits(minified)
	minified = simplifyCalc(minified)
	minified = removeTrailingSemicolons(minified)
	minified = removeEmptyRules(minified)
	minified = removeDuplicateDeclarations(minified)

	return minified
}

// shortenHexColors shortens #rrggbb to #rgb when possible, and #rrggbbaa to #rgba.
func shortenHexColors(css string) string {
	return hexColorRe.ReplaceAllStringFunc(css, func(match string) string {
		if len(match) == 7 { // #rrggbb
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5])
			}
		}
		if len(match) == 9 { // #rrggbbaa
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] && match[7] == match[8] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5]) + string(match[7])
			}
		}
		return match
	})
}

// customPropRe matches a custom property declaration (--name: value). Custom
// property values are substituted verbatim into var()/calc() later, so their
// bytes must never be rewritten by value-level transforms like removeZeroUnits.
var customPropRe = regexp.MustCompile(`(?:^|[;{})\s])--[a-zA-Z0-9_-]+\s*:\s*[^;}]*`)

// removeZeroUnits removes units from 0 values (0px → 0), EXCEPT inside custom
// property values. Rewriting `--x: 0rem` to `--x: 0` changes the substituted
// value — calc(1rem + var(--x)) is only valid when --x carries a unit — so
// custom property declarations are stashed and restored verbatim.
func removeZeroUnits(css string) string {
	type placeholder struct {
		token, value string
	}
	var stash []placeholder
	protect := customPropRe.ReplaceAllStringFunc(css, func(m string) string {
		colon := strings.Index(m, ":")
		if colon < 0 {
			return m
		}
		// Keep any leading boundary character (space/;/{/}) so the declaration
		// still parses, but stash the raw value untouched.
		prefix := m[:colon+1]
		tok := fmt.Sprintf("\x00cp%d\x00", len(stash))
		stash = append(stash, placeholder{tok, m[colon+1:]})
		return prefix + tok
	})
	protect = zeroUnitRe.ReplaceAllString(protect, "0")
	for _, p := range stash {
		protect = strings.ReplaceAll(protect, p.token, p.value)
	}
	return protect
}

// removeEmptyRules removes empty rulesets (selectors with no declarations).
func removeEmptyRules(css string) string {
	return emptyRuleRe.ReplaceAllString(css, "")
}

// rgbaToHex converts rgba(r,g,b,1) and rgb(r,g,b) to #hex.
func rgbaToHex(css string) string {
	css = rgbaRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbaRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		a := parts[4]
		if a != "1" && a != "1.0" && a != "1.00" {
			return match
		}
		r := parseByte(parts[1])
		g := parseByte(parts[2])
		b := parseByte(parts[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	})
	css = rgbRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		r := parseByte(parts[1])
		g := parseByte(parts[2])
		b := parseByte(parts[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	})
	return css
}

func parseByte(s string) uint8 {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}

// simplifyCalc simplifies calc() expressions: calc(0 + X) → X, calc(X + 0) → X, calc(X * 1) → X, etc.
func simplifyCalc(css string) string {
	return calcRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := calcRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		expr := strings.TrimSpace(parts[1])
		lower := strings.ToLower(expr)
		// calc(0 + X) → X
		if strings.HasPrefix(lower, "0 + ") {
			return strings.TrimSpace(expr[4:])
		}
		// calc(X + 0) → X
		if strings.HasSuffix(lower, " + 0") {
			return strings.TrimSpace(expr[:len(expr)-4])
		}
		// calc(0 - X) → -X
		if strings.HasPrefix(lower, "0 - ") {
			return "-" + strings.TrimSpace(expr[4:])
		}
		// calc(X * 1) → X
		if strings.HasSuffix(lower, " * 1") {
			return strings.TrimSpace(expr[:len(expr)-4])
		}
		// calc(X * 0) → 0
		if strings.HasSuffix(lower, " * 0") {
			return "0"
		}
		// calc(1 * X) → X
		if strings.HasPrefix(lower, "1 * ") {
			return strings.TrimSpace(expr[4:])
		}
		return match
	})
}

// removeTrailingSemicolons removes semicolons right before closing braces.
func removeTrailingSemicolons(css string) string {
	return trailingSemiRe.ReplaceAllString(css, "}")
}

// removeDuplicateDeclarations removes duplicate property declarations within a
// rule, keeping only the last declaration for each property. At-rules such as
// @media/@keyframes/@supports are only recurded: their leaf rules are deduped
// independently so nested rules never bleed declarations into one another.
func removeDuplicateDeclarations(css string) string {
	var out strings.Builder
	scanRules(&out, css)
	return out.String()
}

// scanRules walks a CSS chunk, locating rules and their bodies. When a body
// contains nested rules (an at-rule container) it recurses so only LEAF rules
// are deduped; rule ordering, whitespace and at-rule structure are preserved.
func scanRules(out *strings.Builder, css string) {
	for i := 0; i < len(css); {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			out.WriteString(css[i:])
			return
		}
		open += i
		out.WriteString(css[i:open])
		closeBrace, deep := matchBrace(css, open)
		if closeBrace < 0 {
			out.WriteString(css[open:])
			return
		}
		out.WriteByte('{')
		body := css[open+1 : closeBrace]
		if deep {
			scanRules(out, body)
		} else {
			out.WriteString(deduplicateBody(body))
		}
		out.WriteByte('}')
		i = closeBrace + 1
	}
}

// matchBrace finds the brace that closes css[open]. The second return reports
// whether the body contains a nested rule (i.e. at least one deeper '{').
func matchBrace(css string, open int) (closeBrace int, deep bool) {
	depth := 1
	for j := open + 1; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
			if depth == 2 {
				deep = true
			}
		case '}':
			depth--
			if depth == 0 {
				return j, deep
			}
		}
	}
	return -1, deep
}

func deduplicateBody(body string) string {
	props := make(map[string]string)
	var order []string
	for _, raw := range splitDecls(body) {
		prop, val, hasColon := parseDecl(raw)
		if !hasColon {
			continue
		}
		lowerProp := strings.ToLower(prop)
		if _, exists := props[lowerProp]; !exists {
			order = append(order, lowerProp)
		}
		props[lowerProp] = val
	}

	var b strings.Builder
	for i, prop := range order {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(prop)
		b.WriteByte(':')
		b.WriteString(props[prop])
	}
	return b.String()
}

// splitDecls splits a rule body into declaration strings at top-level ';'
// boundaries, ignoring ';' inside quoted strings and inside balanced parens
// (e.g. url("data:image/svg+xml;utf8,...") or var(--x, ...)).
func splitDecls(body string) []string {
	var decls []string
	start := 0
	paren := 0
	var quote byte // 0 = none, '\'' or '"'
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			if c == quote && (i == 0 || body[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ';':
			if paren == 0 {
				decls = append(decls, body[start:i])
				start = i + 1
			}
		}
	}
	if start < len(body) {
		decls = append(decls, body[start:])
	}
	return decls
}

// parseDecl splits one declaration at its first top-level ':' (outside quotes
// and parens), returning the property name, value, and whether a colon existed.
func parseDecl(s string) (prop, val string, hasColon bool) {
	paren := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote && (i == 0 || s[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ':':
			if paren == 0 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return strings.TrimSpace(s), "", false
}

var hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{6,8}\b`)
var zeroUnitRe = regexp.MustCompile(`\b0(px|em|rem|vh|vw|vmin|vmax|%|pt|pc|in|cm|mm|ex|ch|fr)\b`)
var emptyRuleRe = regexp.MustCompile(`[^}]*\{\s*\}`)
var rgbaRe = regexp.MustCompile(`rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(0?\.?\d+|1\.0*)\s*\)`)
var rgbRe = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)`)
var calcRe = regexp.MustCompile(`calc\(([^)]+)\)`)
var trailingSemiRe = regexp.MustCompile(`;\s*\}`)
var blockCommentRe = regexp.MustCompile(`/\*[\s\S]*?\*/`)

func Bundle(assets []*Asset) string {
	var b strings.Builder
	for _, a := range assets {
		if !a.IsModule {
			b.WriteString(a.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func ExtractClassNames(css string) []string {
	seen := make(map[string]bool)
	matches := classSelector.FindAllStringSubmatch(css, -1)
	var result []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			result = append(result, m[1])
		}
	}
	sort.Strings(result)
	return result
}

func GenerateMapping(path string, classNames []string) map[string]string {
	hash := hashPath(path)
	mapping := make(map[string]string, len(classNames))
	for _, name := range classNames {
		mapping[name] = name + "_" + hash
	}
	return mapping
}

func ScopeCSS(content string, mapping map[string]string) string {
	return classSelector.ReplaceAllStringFunc(content, func(match string) string {
		name := match[1:]
		if scoped, ok := mapping[name]; ok {
			return "." + scoped
		}
		return match
	})
}

func VerifyMapping(mapping map[string]string) error {
	for orig, scoped := range mapping {
		if !strings.HasPrefix(scoped, orig+"_") {
			return fmt.Errorf("invalid scoped name %q for class %q", scoped, orig)
		}
	}
	return nil
}
