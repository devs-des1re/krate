package build

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindLayoutGeneratedPageNestedFallback verifies that a page generated under
// .krate/gen resolves its nearest _layout within the gen tree, and that a gen
// page with no gen-tree layout stays unwrapped (returns "") rather than being
// forced into the app root layout — generated pages bring their own layout.
func TestFindLayoutGeneratedPageNestedFallback(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	genDir := filepath.Join(root, ".krate", "gen")
	if err := os.MkdirAll(filepath.Join(genDir, "docs", "guides"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	rootLayout := filepath.Join(pagesDir, "_layout.tsx")
	if err := os.WriteFile(rootLayout, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}
	nestedLayout := filepath.Join(genDir, "docs", "_layout.tsx")
	if err := os.WriteFile(nestedLayout, []byte("docs"), 0644); err != nil {
		t.Fatal(err)
	}

	deepPage := filepath.Join(genDir, "docs", "guides", "deep.tsx")
	if got := findLayout(deepPage, pagesDir); got != nestedLayout {
		t.Errorf("findLayout(deep) = %q, want nested %q", got, nestedLayout)
	}

	shallowPage := filepath.Join(genDir, "docs", "intro.tsx")
	if got := findLayout(shallowPage, pagesDir); got != nestedLayout {
		t.Errorf("findLayout(shallow) = %q, want nested %q", got, nestedLayout)
	}

	// A gen page with no gen-local layout must NOT fall back to pages/_layout.tsx
	// — it stays unwrapped (the app root layout belongs to regular pages only).
	orphan := filepath.Join(root, ".krate", "gen", "misc", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(orphan), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := findLayout(orphan, pagesDir); got != "" {
		t.Errorf("findLayout(orphan) = %q, want empty (no app-root fallback for gen pages)", got)
	}
}

// TestFindLayoutRegularPage verifies normal pages under pagesDir still resolve
// their nearest layout and the pagesDir root layout.
func TestFindLayoutRegularPage(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	blog := filepath.Join(pagesDir, "blog")
	if err := os.MkdirAll(blog, 0755); err != nil {
		t.Fatal(err)
	}
	rootLayout := filepath.Join(pagesDir, "_layout.tsx")
	if err := os.WriteFile(rootLayout, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}
	blogLayout := filepath.Join(blog, "_layout.tsx")
	if err := os.WriteFile(blogLayout, []byte("blog"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := findLayout(filepath.Join(blog, "post.tsx"), pagesDir); got != blogLayout {
		t.Errorf("nested regular layout = %q, want %q", got, blogLayout)
	}
	if got := findLayout(filepath.Join(pagesDir, "home.tsx"), pagesDir); got != rootLayout {
		t.Errorf("root regular layout = %q, want %q", got, rootLayout)
	}
}
