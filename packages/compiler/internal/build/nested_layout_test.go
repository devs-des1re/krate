package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestLayoutImportsNavbar verifies a _layout.tsx can import and render a shared
// component (e.g. Navbar) from another module: the imported component must emit
// its markup inside the layout shell around {children}. Regression for the
// "nested layouts (navbar in layout)" report area.
func TestLayoutImportsNavbar(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("src/components/Navbar.tsx", `export function Navbar() {
  return <nav class="top-nav"><a href="/">Home</a><a href="/about">About</a></nav>;
}
`)
	write("src/pages/_layout.tsx", `import { Navbar } from "../components/Navbar";

export default function Layout(children) {
  return (
    <div class="layout">
      <Navbar />
      <main>{children}</main>
    </div>
  );
}
`)
	write("src/pages/index.tsx", `export default function Index() {
  return <p id="page-body">welcome home</p>;
}
`)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("index.html not emitted: %v", err)
	}
	html := string(data)
	for _, want := range []string{`<nav class="top-nav">`, `<a href="/">Home</a>`, `<main>`, `welcome home`} {
		if !strings.Contains(html, want) {
			t.Errorf("emitted HTML missing %q:\n%s", want, html)
		}
	}
	// The navbar (from an imported module) must appear before the page body,
	// proving both the imported component and {children} rendered.
	navIdx := strings.Index(html, `<nav class="top-nav">`)
	mainIdx := strings.Index(html, `welcome home`)
	if navIdx < 0 || mainIdx < 0 || navIdx > mainIdx {
		t.Errorf("navbar not rendered before page content (nav %d, body %d):\n%s", navIdx, mainIdx, html)
	}
}
