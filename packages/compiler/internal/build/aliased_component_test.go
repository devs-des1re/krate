package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestBuildAliasedComponentEmission verifies an aliased (`@/...`) imported
// component is resolved by the bundler, merged into the annotation set, and
// inlined into the built page HTML instead of being silently dropped.
// Regression: resolvePathAlias only tried baseUrl-relative targets (double
// "src/" baseUrl + "src/*" target), and MergeModuleFunctions skipped re-walking
// already-used page-local components, so <Badge/> vanished from the output.
func TestBuildAliasedComponentEmission(t *testing.T) {
	root := testProjectPath(t)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.OutDir = filepath.Join(t.TempDir(), "dist")
	b := New(root, cfg)
	res, _, err := b.buildPage(filepath.Join(root, "src", "pages", "syntax-robustness.tsx"))
	if err != nil {
		t.Fatalf("buildPage: %v", err)
	}
	if !strings.Contains(res.HTML, `<span class="badge">fragment + spread attrs</span>`) {
		t.Errorf("alias-imported <Badge> dropped from HTML:\n%.600s", res.HTML)
	}
	if !strings.Contains(res.HTML, `<span class="badge">fragments render children</span>`) {
		t.Errorf("second alias-imported <Badge> dropped from HTML:\n%.600s", res.HTML)
	}
}
