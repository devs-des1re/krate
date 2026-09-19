package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Validation issues are split into hard errors (invalid values that will
// misbehave) and warnings (unknown keys, suspicious values). Load surfaces both.

// knownTopLevelKeys is the set of recognized top-level config keys, derived from
// the Config struct's JSON tags. Used to warn on typos/unknown keys that JSON
// unmarshalling would otherwise drop silently.
var knownTopLevelKeys = map[string]bool{
	"entry": true, "outDir": true, "pagesDir": true, "publicDir": true,
	"minify": true, "minifyHTML": true, "minifyCSS": true, "minifyJS": true,
	"sourcemap": true, "devServer": true, "plugins": true, "emitReact": true,
	"markdown": true, "tailwind": true, "csp": true, "runtime": true, "ssr": true,
	"pathAliases": true, "tsBaseDir": true, "redirects": true, "rewrites": true,
	"seo": true, "robots": true, "output": true, "serverComponents": true,
	"runtimeComponents": true, "serverDirs": true, "runtimeDirs": true,
	"content": true, "checks": true,
}

// Validate performs semantic/bounds checks that JSON typing alone can't. It
// returns a fatal error for values that would break a build, and a list of
// warnings for suspicious-but-tolerable values.
func (c *Config) Validate() (warnings []string, err error) {
	if c.Output != "" && c.Output != "static" {
		return nil, fmt.Errorf("output must be \"\" or \"static\", got %q", c.Output)
	}
	if c.DevServer.Port < 0 || c.DevServer.Port > 65535 {
		return nil, fmt.Errorf("devServer.port must be between 0 and 65535, got %d", c.DevServer.Port)
	}
	if c.SSR.RendererPort < 0 || c.SSR.RendererPort > 65535 {
		return nil, fmt.Errorf("ssr.rendererPort must be between 0 and 65535, got %d", c.SSR.RendererPort)
	}
	if c.SSR.Timeout < 0 {
		return nil, fmt.Errorf("ssr.timeout must be >= 0, got %d", c.SSR.Timeout)
	}
	if c.SSR.MaxCacheSize < 0 {
		return nil, fmt.Errorf("ssr.maxCacheSize must be >= 0, got %d", c.SSR.MaxCacheSize)
	}
	if c.SSR.Streaming && c.Output == "static" {
		warnings = append(warnings, "ssr.streaming is ignored when output is \"static\"")
	}

	for _, rd := range c.Redirects {
		if !strings.HasPrefix(rd.Source, "/") {
			warnings = append(warnings, fmt.Sprintf("redirect source %q should start with /", rd.Source))
		}
	}
	for _, rw := range c.Rewrites {
		if !strings.HasPrefix(rw.Source, "/") {
			warnings = append(warnings, fmt.Sprintf("rewrite source %q should start with /", rw.Source))
		}
	}

	if c.SSR.RendererPort != 0 && c.SSR.RendererPort == c.DevServer.Port {
		warnings = append(warnings, "ssr.rendererPort equals devServer.port; choose a distinct port")
	}

	return warnings, nil
}

// UnknownKeyWarnings compares the raw config JSON against the known key set and
// returns a warning per unrecognized top-level key. This catches typos that the
// typed unmarshal silently drops.
func UnknownKeyWarnings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var unknown []string
	for k := range m {
		if k == "validate" { // user-supplied hook, intentionally not in Config
			continue
		}
		if !knownTopLevelKeys[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	var out []string
	for _, k := range unknown {
		out = append(out, fmt.Sprintf("unknown config key %q (ignored)", k))
	}
	return out
}
