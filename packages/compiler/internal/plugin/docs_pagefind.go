package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// pagefindTimeout bounds the Pagefind indexing subprocess. Pagefind is opt-in
// and only runs on production builds; the hook itself allows more headroom (see
// afterBuildHookTimeout), so this can be generous.
const pagefindTimeout = 4 * time.Minute

// afterBuild runs site-wide post-build work. Today this is the opt-in Pagefind
// indexer: Pagefind indexes built HTML, so it must run after every page has been
// written. Any failure is a warning (the client falls back to docfind/JSON) —
// Pagefind is an enhancement, never a hard build requirement.
func (p *DocsPlugin) afterBuild(ctx *BuildResultHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}
	opts := parseDocsOptions(cfg)
	if opts == nil {
		return nil
	}

	enabled, engine, _ := searchConfig(opts)
	if !enabled || engine != "pagefind" || ctx.DevMode {
		return nil
	}

	if err := runPagefind(ctx.Root, ctx.OutDir, pagefindOptions(opts)); err != nil {
		fmt.Fprintf(os.Stderr, "  Docs search warning: pagefind: %v (falling back to JSON search)\n", err)
	}
	return nil
}

// runPagefind runs `npx pagefind` over the built site, writing the bundle to the
// configured output subdirectory. Pagefind is a native binary distributed via
// npm; if npx/Node/network is unavailable the error is surfaced to the caller,
// which downgrades it to a warning.
func runPagefind(root, outDir string, opts PagefindOptions) error {
	args := pagefindArgs(outDir, opts)

	ctx, cancel := context.WithTimeout(context.Background(), pagefindTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, lastLine(msg))
		}
		return err
	}
	return nil
}

// pagefindArgs builds the argument vector for `npx`, i.e. everything after
// "npx". It always requests the pinned-major Pagefind CLI and points it at the
// built site.
func pagefindArgs(outDir string, opts PagefindOptions) []string {
	subdir := opts.OutputSubdir
	if subdir == "" {
		subdir = "pagefind"
	}
	args := []string{"--yes", "pagefind", "--site", outDir, "--output-subdir", subdir}
	if len(opts.ExcludeSelectors) > 0 {
		args = append(args, "--exclude-selectors", strings.Join(opts.ExcludeSelectors, ", "))
	}
	if opts.IncludeCharacters != "" {
		args = append(args, "--include-characters", opts.IncludeCharacters)
	}
	if opts.ForceLanguage != "" {
		args = append(args, "--force-language", opts.ForceLanguage)
	}
	if opts.Verbose {
		args = append(args, "--verbose")
	}
	return args
}

// lastLine returns the final non-empty line of s, used to keep multi-line
// subprocess output from flooding the build log.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
