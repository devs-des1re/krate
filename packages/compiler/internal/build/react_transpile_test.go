package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildReactPage writes a single-page project to a temp dir and returns the
// output directory plus the page's emitted HTML and hydration JS.
func buildReactPage(t *testing.T, pageSrc string) (html, js string) {
	t.Helper()
	root := t.TempDir()
	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageSrc), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	// Keep identifiers stable so the emitted JS is assertable.
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	// Static pages correctly emit no hydration bundle; JS is empty then.
	jsFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil {
		t.Fatalf("glob hydration bundle: %v", err)
	}
	jsBytes := []byte{}
	if len(jsFiles) > 0 {
		jsBytes, err = os.ReadFile(jsFiles[0])
		if err != nil {
			t.Fatal(err)
		}
	}
	return string(htmlBytes), string(jsBytes)
}

// TestReactUseRefNoDuplicateVar is the regression for the emitted hydration JS
// containing both `var r={current:null}` and a clobbering `var r="{current:null}"`.
func TestReactUseRefNoDuplicateVar(t *testing.T) {
	page := `
		import { useState, useRef } from 'react';
		export default function Page() {
			const ref = useRef(null);
			const [n, setN] = useState(0);
			return <div ref={ref}>{n}</div>;
		}
	`
	_, js := buildReactPage(t, page)
	if strings.Contains(js, `="{current:null}"`) {
		t.Fatalf("useRef object was clobbered by a duplicate string var:\n%s", js)
	}
	// Exactly one declaration of the ref variable.
	if c := strings.Count(js, "var ref="); c != 1 {
		t.Errorf("expected one 'var ref=' declaration, got %d:\n%s", c, js)
	}
}

// TestReactBareReadInHandlerBuild verifies unmodified React (`count + 1`) in a
// handler compiles to a live getter read at build time.
func TestReactBareReadInHandlerBuild(t *testing.T) {
	page := `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(5);
			return <button onClick={() => setCount(count + 1)}>{count}</button>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, ">5<") {
		t.Errorf("SSR should render initial count 5:\n%.400s", html)
	}
	if !strings.Contains(js, "count()") {
		t.Errorf("bare read should compile to a call:\n%s", js)
	}
}

// TestReactKeyNotEmitted verifies `key` is stripped from intrinsic elements.
func TestReactKeyNotEmitted(t *testing.T) {
	page := `
		export default function Page() {
			return <div key="abc" data-x="1">hi</div>;
		}
	`
	html, _ := buildReactPage(t, page)
	if strings.Contains(html, "key=") {
		t.Errorf("key must not be emitted to the DOM:\n%.400s", html)
	}
}

// TestReactUseReducerBuild verifies useReducer lowers to createReducer in the
// hydration JS and the state getter renders its SSR initial.
func TestReactUseReducerBuild(t *testing.T) {
	page := `
		import { useReducer } from 'react';
		export default function Page() {
			const [count, dispatch] = useReducer((s, a) => s + a, 7);
			return <button onClick={() => dispatch(1)}>{count}</button>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, ">7<") {
		t.Errorf("SSR should render reducer initial 7:\n%.400s", html)
	}
	if !strings.Contains(js, "createReducer") {
		t.Errorf("hydration should emit createReducer:\n%s", js)
	}
}

// TestReactUseIdBuild verifies useId lowers to a stable per-instance literal
// used for both the id and htmlFor attributes, with no runtime useId call.
func TestReactUseIdBuild(t *testing.T) {
	page := `
		import { useId } from 'react';
		export default function Page() {
			const id = useId();
			return <div><label htmlFor={id}>Name</label><input id={id} /></div>;
		}
	`
	html, js := buildReactPage(t, page)
	if strings.Contains(js, "useId") {
		t.Errorf("useId should be compile-time resolved:\n%s", js)
	}
	// The same generated id must appear on both the label and input.
	if !strings.Contains(html, `for="krate-`) || !strings.Contains(html, `id="krate-`) {
		t.Errorf("expected matching for/id literals:\n%.500s", html)
	}
}

// TestReactFragmentBuild verifies <Fragment> renders its children inline.
func TestReactFragmentBuild(t *testing.T) {
	page := `
		import { Fragment } from 'react';
		export default function Page() {
			return <Fragment><span>a</span><span>b</span></Fragment>;
		}
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "<span>a</span><span>b</span>") {
		t.Errorf("fragment children should render inline:\n%.400s", html)
	}
}

// TestReactStyleObjectBuild verifies a literal style object folds to CSS.
func TestReactStyleObjectBuild(t *testing.T) {
	page := `
		export default function Page() {
			return <div style={{ fontSize: 14, backgroundColor: 'red' }}>hi</div>;
		}
	`
	html, _ := buildReactPage(t, page)
	if !strings.Contains(html, "font-size:14px") || !strings.Contains(html, "background-color:red") {
		t.Errorf("style object should fold to CSS:\n%.400s", html)
	}
}
