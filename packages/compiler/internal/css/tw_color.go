package css

import (
	"strconv"
	"strings"
)

// splitColorModifier splits a color utility's value into the color key and an
// optional alpha modifier: "blue-500/50" → ("blue-500", "50"); "blue-500" →
// ("blue-500", ""). Bracketed values may contain `/`, so brackets are honoured.
func splitColorModifier(v string) (color, alpha string) {
	depth := 0
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '/':
			if depth == 0 {
				return v[:i], v[i+1:]
			}
		}
	}
	return v, ""
}

// resolveAlpha resolves an alpha modifier to a CSS color-alpha component:
// "50" → "0.5", "[0.42]" → "0.42", "75" → "0.75". Returns ok=false for a
// non-numeric modifier.
func resolveAlpha(a string) (string, bool) {
	if a == "" {
		return "", false
	}
	if strings.HasPrefix(a, "[") && strings.HasSuffix(a, "]") {
		return a[1 : len(a)-1], true
	}
	if n, err := strconv.Atoi(a); err == nil && n >= 0 && n <= 100 {
		// Represent as a decimal without trailing zeros.
		f := float64(n) / 100
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}

// colorValue looks up a color key (e.g. "blue-500", "white", "transparent") in
// the theme and returns the CSS color. When alpha is set and the color is a hex
// value it is re-emitted as `rgb(r g b / a)`.
func colorValue(key, alpha string, theme TailwindTheme) (string, bool) {
	if v, ok := theme.BgColors[key]; ok {
		if a, ok := resolveAlpha(alpha); ok {
			return withAlpha(v, a), true
		}
		return v, true
	}
	// shade form: <name>-<shade>
	if name, shade, ok := splitColorShade(key); ok {
		if shades, ok := theme.Colors[name]; ok {
			if hex, ok := shades[shade]; ok {
				if a, ok := resolveAlpha(alpha); ok {
					return withAlpha(hex, a), true
				}
				return hex, true
			}
		}
	}
	// arbitrary: [#ff0000]/[rgb(...)]
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
		val := key[1 : len(key)-1]
		if a, ok := resolveAlpha(alpha); ok {
			return withAlpha(val, a), true
		}
		return val, true
	}
	return "", false
}

// splitColorShade splits "blue-500" into ("blue","500"); a non-shade key returns
// ok=false.
func splitColorShade(key string) (name, shade string, ok bool) {
	i := strings.LastIndexByte(key, '-')
	if i <= 0 {
		return "", "", false
	}
	name, shade = key[:i], key[i+1:]
	if shade == "" {
		return "", "", false
	}
	for _, r := range shade {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}
	return name, shade, true
}

// withAlpha converts a hex or rgb color to an `rgb(r g b / a)` form when the
// input is a 3/6-digit hex; other values are returned unchanged.
func withAlpha(color, alpha string) string {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return color
	}
	r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return color
	}
	return "rgb(" + strconv.Itoa(int(r)) + " " + strconv.Itoa(int(g)) + " " + strconv.Itoa(int(b)) + " / " + alpha + ")"
}

// negateValue returns the negated form of a length/number value, preserving
// units: "1rem" → "-1rem", "0" → "-0" (rendered "0").
func negateValue(v string) string {
	if v == "" {
		return v
	}
	if strings.HasPrefix(v, "-") {
		return strings.TrimPrefix(v, "-")
	}
	return "-" + v
}
