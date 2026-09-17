package css

import (
	"os"
	"regexp"
	"strings"
)

// ParseTailwindConfigStatic parses a tailwind.config.{ts,js,mjs} file without
// executing it, extracting the common `theme` / `extend` / `darkMode` shapes.
// It deliberately handles a useful subset (object/array/string literals) so the
// Go-native build needs no Node; anything it cannot parse returns ok=false and
// the caller may fall back to `npx tsx`.
func ParseTailwindConfigStatic(path string) (*TailwindConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	src := string(data)

	cfg := &TailwindConfig{Theme: DefaultTailwindTheme()}

	// darkMode: 'class' | 'media' | ['class', ...]
	if m := reDarkMode.FindStringSubmatch(src); m != nil {
		v := m[1]
		if v == "class" || v == "selector" || v == "media" {
			cfg.Theme.DarkMode = v
			cfg.DarkMode = v
		}
	}

	// Locate `theme: { ... }`.
	themeSrc, ok := extractBlock(src, "theme")
	if !ok {
		// No theme object: the config still parsed (defaults + darkMode).
		return cfg, true
	}

	// Top-level theme keys replace defaults; `extend` keys merge.
	override, extend := splitThemeBlocks(themeSrc)
	applyThemeKey(cfg, "spacing", override, extend, func(m map[string]string) { cfg.Theme.Spacing = m }, defaultSpacing)
	applyThemeKey(cfg, "screens", override, extend, func(m map[string]string) { cfg.Theme.Screens = m }, defaultScreens)
	applyThemeKey(cfg, "maxWidth", override, extend, func(m map[string]string) { cfg.Theme.MaxWidth = m }, defaultMaxWidth)
	applyThemeKey(cfg, "minWidth", override, extend, func(m map[string]string) { cfg.Theme.MinWidth = m }, defaultMinWidth)
	applyThemeKey(cfg, "lineHeight", override, extend, func(m map[string]string) { cfg.Theme.LineHeight = m }, defaultLineHeight)
	applyThemeKey(cfg, "opacity", override, extend, func(m map[string]string) { cfg.Theme.Opacity = m }, defaultOpacity)
	applyThemeKey(cfg, "fontFamily", override, extend, func(m map[string]string) { cfg.Theme.FontFamily = m }, defaultFontFamily)
	applyThemeKey(cfg, "borderRadius", override, extend, func(m map[string]string) { cfg.Theme.Radii = m }, nil)
	applyThemeKey(cfg, "boxShadow", override, extend, func(m map[string]string) { cfg.Theme.Shadows = m }, nil)
	applyThemeKey(cfg, "fontSize", override, extend, func(m map[string]string) { cfg.Theme.TextSizes = m }, nil)
	applyThemeKey(cfg, "fontWeight", override, extend, func(m map[string]string) { cfg.Theme.FontWeights = m }, nil)

	// Colors: nested map (name → shade → value). Replaced when top-level.
	if colors, ok := parseColorMap(override, "colors"); ok {
		cfg.Theme.Colors = colors
	} else if colors, ok := parseColorMap(extend, "colors"); ok {
		for k, v := range colors {
			cfg.Theme.Colors[k] = v
		}
	}

	return cfg, true
}

var (
	reDarkMode = regexp.MustCompile(`darkMode\s*:\s*['"]([a-zA-Z]+)['"]`)
)

// splitThemeBlocks extracts the top-level theme source and the `extend` source.
func splitThemeBlocks(themeSrc string) (override, extend string) {
	if ext, ok := extractBlock(themeSrc, "extend"); ok {
		// Remove the extend block from the override source.
		override = removeBlock(themeSrc, "extend")
		return override, ext
	}
	return themeSrc, ""
}

// applyThemeKey parses a flat string/number map under `key` from the override
// block (replace) or the extend block (merge onto defaults).
func applyThemeKey(cfg *TailwindConfig, key, override, extend string, set func(map[string]string), defaults func() map[string]string) {
	if m, ok := parseFlatMap(override, key); ok && len(m) > 0 {
		set(m)
		return
	}
	if m, ok := parseFlatMap(extend, key); ok && len(m) > 0 {
		base := defaults
		var merged map[string]string
		if base != nil {
			merged = base()
		} else {
			merged = map[string]string{}
		}
		for k, v := range m {
			merged[k] = v
		}
		set(merged)
	}
}

// parseFlatMap parses `key: { a: 'x', b: 4 }` into a string map.
func parseFlatMap(src, key string) (map[string]string, bool) {
	block, ok := extractBlock(src, key)
	if !ok {
		return nil, false
	}
	out := map[string]string{}
	for _, m := range reKV.FindAllStringSubmatch(block, -1) {
		out[unquote(m[1])] = unquote(m[2])
	}
	return out, len(out) > 0
}

// parseColorMap parses `colors: { name: { shade: 'value' } }` (or a flat
// `colors: { name: 'value' }`).
func parseColorMap(src, key string) (map[string]map[string]string, bool) {
	block, ok := extractBlock(src, key)
	if !ok {
		return nil, false
	}
	out := map[string]map[string]string{}
	for _, m := range reNestedKV.FindAllStringSubmatch(block, -1) {
		name := unquote(m[1])
		inner := m[2]
		shades := map[string]string{}
		for _, sm := range reKV.FindAllStringSubmatch(inner, -1) {
			shades[unquote(sm[1])] = unquote(sm[2])
		}
		if len(shades) > 0 {
			out[name] = shades
		}
	}
	return out, len(out) > 0
}

var (
	reKV       = regexp.MustCompile(`['"]?([A-Za-z0-9_-]+)['"]?\s*:\s*(['"][^'"]*['"]|[0-9.]+)`)
	reNestedKV = regexp.MustCompile(`['"]?([A-Za-z0-9_-]+)['"]?\s*:\s*\{([^{}]*)\}`)
)

// extractBlock finds `key: { ... }` and returns the inner source, honouring
// nested braces and string literals.
func extractBlock(src, key string) (string, bool) {
	re := regexp.MustCompile(`(['"]?` + regexp.QuoteMeta(key) + `['"]?)\s*:\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return "", false
	}
	start := loc[1] - 1 // index of '{'
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start+1 : i], true
			}
		case '\'', '"', '`':
			// Skip the string literal.
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return "", false
}

// removeBlock removes `key: { ... }` from src.
func removeBlock(src, key string) string {
	re := regexp.MustCompile(`(['"]?` + regexp.QuoteMeta(key) + `['"]?)\s*:\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return src
	}
	start := loc[1] - 1
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[:loc[0]] + src[i+1:]
			}
		case '\'', '"', '`':
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return src
}

// unquote strips surrounding quotes from a config value token.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
