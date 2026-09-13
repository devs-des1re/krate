package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// newTestService builds a Service over a temp project with one page.
func newTestService(t *testing.T, files map[string]string) *Service {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	return NewService(Options{Root: root, Cfg: cfg})
}

// serve drives the server with newline-delimited JSON requests and returns the
// decoded responses.
func serve(t *testing.T, svc *Service, requests ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	srv := NewServer(&out, "krate", "test")
	svc.Register(srv)

	input := strings.Join(requests, "\n") + "\n"
	if err := srv.Serve(strings.NewReader(input)); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var responses []map[string]any
	dec := json.NewDecoder(&out)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	return responses
}

func call(name string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	return string(b)
}

func toolText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func TestInitializeAndToolsList(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	if resps[0]["result"] == nil {
		t.Fatalf("initialize failed: %v", resps[0])
	}
	result := resps[1]["result"].(map[string]any)
	tools := result["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"list_routes", "read_page", "read_content", "create_page", "edit_ast", "edit_page", "build", "check", "search_docs"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestListRoutes(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":      "export default function Page() { return <h1>Hi</h1>; }",
		"src/pages/about.tsx":      "export default function Page() { return <h1>About</h1>; }",
		"src/pages/video/[id].tsx": "export default function Page() { return <h1>Video</h1>; }",
	})
	resps := serve(t, svc, call("list_routes", nil))
	text := toolText(t, resps[0])
	if !strings.Contains(text, `"/about"`) {
		t.Errorf("expected /about route, got %s", text)
	}
	if !strings.Contains(text, `"id"`) {
		t.Errorf("expected dynamic param id, got %s", text)
	}
}

func TestReadPage(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function About() { return <h1>About</h1>; }",
	})
	// The default format is the raw source (no AST round-trip, no absolute path).
	resps := serve(t, svc, call("read_page", map[string]any{"route": "/about"}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, `"content": "export default function About()`) {
		t.Errorf("expected source content, got %s", text)
	}
	if strings.Contains(text, `"kind": "Program"`) {
		t.Errorf("source format should not include the AST, got %s", text)
	}
	if strings.Contains(text, "sourcePath") {
		t.Errorf("sourcePath must not leak across the bridge: %s", text)
	}

	// format "all" returns source, AST, and lossyTypes.
	resps = serve(t, svc, call("read_page", map[string]any{"route": "/about", "format": "all"}))
	text = toolText(t, resps[0])
	if !strings.Contains(text, `"lossyTypes": false`) {
		t.Errorf("expected lossyTypes false, got %s", text)
	}
	if !strings.Contains(text, `"kind": "Program"`) {
		t.Errorf("expected AST document, got %s", text)
	}
	if !strings.Contains(text, `"content":`) {
		t.Errorf("expected source content in all format, got %s", text)
	}

	// An invalid format is rejected.
	text = toolText(t, serve(t, svc, call("read_page", map[string]any{"route": "/about", "format": "bogus"}))[0])
	if !strings.Contains(text, "format must be one of") {
		t.Errorf("expected format validation, got %s", text)
	}
}

func TestCreatePageDryRunThenApply(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})

	// Dry-run: no file written.
	resps := serve(t, svc, call("create_page", map[string]any{"route": "/pricing"}))
	if !strings.Contains(toolText(t, resps[0]), "+++") {
		t.Errorf("expected a diff, got %s", toolText(t, resps[0]))
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "pricing.tsx")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not create the file")
	}

	// Apply.
	resps = serve(t, svc, call("create_page", map[string]any{"route": "/pricing", "apply": true}))
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "pricing.tsx")); err != nil {
		t.Fatalf("apply should create the file: %v", err)
	}
	_ = resps
}

func TestEditASTRefusesLossySource(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/typed.tsx": "interface Props { a: string }\nexport default function Page(props: Props) { return <h1>Hi</h1>; }",
	})
	resps := serve(t, svc, call("edit_ast", map[string]any{"route": "/typed", "ast": `{"kind":"Program","body":[]}`}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, "refusing to edit") {
		t.Fatalf("expected refusal for typed source, got %s", text)
	}
}

func TestEditASTDryRun(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/plain.tsx": "export default function Page() { return <h1>Old</h1>; }",
	})
	// A minimal program: export default function Page() { return <h1>New</h1>; }
	ast := `{"kind":"Program","body":[{"kind":"ExportStmt","default":true,"declaration":{"kind":"FnDecl","name":"Page","body":[{"kind":"ReturnStmt","value":{"kind":"JSXElement","opening":{"kind":"JSXOpening","name":"h1","selfClosing":false},"closing":{"kind":"JSXClosing","name":"h1"},"children":[{"kind":"JSXText","value":"New"}]}}]}}]}`
	resps := serve(t, svc, call("edit_ast", map[string]any{"route": "/plain", "ast": ast}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, "+export default function Page()") {
		t.Fatalf("expected updated source in diff, got %s", text)
	}
	// File unchanged after dry-run.
	data, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "plain.tsx"))
	if !strings.Contains(string(data), "Old") {
		t.Fatal("dry-run must not modify the file")
	}
}

func TestResourceRoutesAndPage(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function About() { return <h1>About</h1>; }",
	})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"krate://routes"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"krate://page/about"}}`,
	)
	if len(resps) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(resps))
	}
	list := resps[0]["result"].(map[string]any)["resources"].([]any)
	if len(list) == 0 {
		t.Fatal("expected registered resources")
	}
	routes := resps[1]["result"].(map[string]any)["contents"].([]any)
	if len(routes) != 1 {
		t.Fatalf("expected routes contents, got %v", routes)
	}
	page := resps[2]["result"].(map[string]any)["contents"].([]any)
	pageText := page[0].(map[string]any)["text"].(string)
	if !strings.Contains(pageText, `"source": "src/pages/about.tsx"`) {
		t.Fatalf("expected about page resource, got %s", pageText)
	}
}

func TestUnknownMethodAndTool(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"does/not/exist"}`,
		call("nope", nil),
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	for i, r := range resps {
		if r["error"] == nil {
			t.Errorf("response %d should be an error: %v", i, r)
		}
	}
}

func TestNotificationNoResponse(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resps) != 0 {
		t.Fatalf("notifications must not produce a response, got %v", resps)
	}
}

func TestUnifiedDiffNewFile(t *testing.T) {
	d := unifiedDiff("pricing.tsx", "", "a\nb\n")
	if !strings.Contains(d, "--- /dev/null") || !strings.Contains(d, "+a") || !strings.Contains(d, "+b") {
		t.Fatalf("unexpected diff: %s", d)
	}
}

func mustResp(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp["error"] != nil {
		t.Fatalf("unexpected error response: %v", resp)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	return result
}

func TestToolAnnotations(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := mustResp(t, serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)[0])
	want := map[string]bool{
		"list_routes": false, "read_page": false, "read_content": false, "search_docs": false,
		"create_page": false, "edit_ast": true, "edit_page": true,
		"build": false, "check": false,
	}
	for _, tool := range resp["tools"].([]any) {
		m := tool.(map[string]any)
		name := m["name"].(string)
		anns, ok := m["annotations"].(map[string]any)
		if !ok {
			t.Errorf("tool %s missing annotations", name)
			continue
		}
		destructive, _ := anns["destructiveHint"].(bool)
		if destructive != want[name] {
			t.Errorf("tool %s destructiveHint = %v, want %v", name, destructive, want[name])
		}
	}
}

func TestVersionNegotiation(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
	)[0])
	if got := resp["protocolVersion"]; got != "2024-11-05" {
		t.Errorf("negotiated protocolVersion = %v, want 2024-11-05", got)
	}
	if _, ok := resp["capabilities"].(map[string]any); !ok {
		t.Errorf("missing capabilities: %v", resp)
	}
}

func TestResourceTemplatesAndCompletions(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"completions/complete","params":{"ref":{"type":"ref/resource","uri":"krate://page/{route}"},"argument":{"name":"route","value":""}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"krate://page/about"}}`,
	)
	tmpls := mustResp(t, resps[0])["resourceTemplates"].([]any)
	if len(tmpls) == 0 {
		t.Fatal("expected registered resource templates")
	}
	first := tmpls[0].(map[string]any)
	if first["uriTemplate"] == nil {
		t.Errorf("template missing uriTemplate: %v", first)
	}

	completion := mustResp(t, resps[1])
	values := completion["completion"].(map[string]any)["values"].([]any)
	found := false
	for _, v := range values {
		if v.(string) == "/about" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /about in route completions, got %v", values)
	}

	page := mustResp(t, resps[2])["contents"].([]any)
	if !strings.Contains(page[0].(map[string]any)["text"].(string), `"source": "src/pages/about.tsx"`) {
		t.Errorf("unexpected page resource: %v", page)
	}
}

func TestPromptsListAndGet(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"add-page","arguments":{"route":"/pricing"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"completions/complete","params":{"ref":{"type":"ref/prompt","name":"add-page"},"argument":{"name":"template","value":""}}}`,
	)
	list := mustResp(t, resps[0])["prompts"].([]any)
	names := map[string]bool{}
	for _, p := range list {
		names[p.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"add-page", "publish-content", "fix-checks", "explore"} {
		if !names[want] {
			t.Errorf("missing prompt %q", want)
		}
	}

	message := mustResp(t, resps[1])["messages"].([]any)
	text := message[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(text, "create_page") || !strings.Contains(text, "/pricing") {
		t.Errorf("unexpected add-page message: %s", text)
	}

	completion := mustResp(t, resps[2])
	values := completion["completion"].(map[string]any)["values"].([]any)
	if strings.Contains(values[0].(string), "static") {
		t.Logf("template completions: %v", values)
	}
}

func TestEditPageFindReplaceDryRunAndApply(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})

	// Dry-run: no file written.
	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "/about", "find": "About", "replace": "Company",
	}))[0])
	if !strings.Contains(text, "+++") || !strings.Contains(text, "Dry run") {
		t.Fatalf("expected dry-run diff, got %s", text)
	}
	about, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "About") {
		t.Fatal("dry-run must not modify the file")
	}

	// Apply writes the change.
	text = toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "/about", "find": "About", "replace": "Company", "apply": true,
	}))[0])
	if !strings.Contains(text, "Updated") || !strings.Contains(text, "+++") {
		t.Fatalf("expected update diff, got %s", text)
	}
	about, _ = os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "Company") {
		t.Errorf("expected edited about page, got %s", about)
	}
}

func TestEditPageFullReplaceAndNewFile(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	// Full-file replace via a project-relative path.
	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "src/pages/index.tsx", "content": "export default function Page() { return <h1>New</h1>; }\n",
	}))[0])
	if !strings.Contains(text, "<h1>Hi</h1>") || !strings.Contains(text, "<h1>New</h1>") {
		t.Fatalf("expected replace diff, got %s", text)
	}

	// content on a missing target creates the file (apply).
	text = toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "src/pages/faq.tsx", "content": "export default function Page() { return <h1>FAQ</h1>; }\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "Created") {
		t.Fatalf("expected create message, got %s", text)
	}
	faq, err := os.ReadFile(filepath.Join(svc.root, "src", "pages", "faq.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(faq), "FAQ") {
		t.Errorf("unexpected faq content: %s", faq)
	}
}

func TestEditPageFindReplaceContextRules(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>a</h1>; return <h1>a</h1>; }",
	})
	// Multiple matches require replaceAll.
	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "/", "find": "<h1>a</h1>", "replace": "<h1>b</h1>",
	}))[0])
	if !strings.Contains(text, "replaceAll: true") {
		t.Fatalf("expected replaceAll hint, got %s", text)
	}

	// replaceAll applies everywhere.
	text = toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "/", "find": "<h1>a</h1>", "replace": "<h1>b</h1>", "replaceAll": true, "apply": true,
	}))[0])
	if !strings.Contains(text, "Updated") {
		t.Fatalf("expected update, got %s", text)
	}

	// A non-matching find text errors.
	text = toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "/", "find": "nope", "replace": "x",
	}))[0])
	if !strings.Contains(text, "not found") {
		t.Fatalf("expected not-found message, got %s", text)
	}
}

func TestEditPageRefusesBrokenSource(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})
	// Replacing "function Page()" with "function Page(" yields unparseable code.
	for _, apply := range []bool{false, true} {
		text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
			"route": "/about", "find": "function Page()", "replace": "function Page(", "apply": apply,
		}))[0])
		if !strings.Contains(text, "refusing to write") {
			t.Fatalf("expected parse refusal (apply=%v), got %s", apply, text)
		}
	}
	about, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "About") {
		t.Errorf("file should be unchanged, got %s", about)
	}
}

func TestEditPageRejectsTraversal(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "../../../evil.tsx", "content": "export default function Page() { return null; }\n",
	}))[0])
	if !strings.Contains(text, "invalid path") {
		t.Fatalf("expected traversal rejection, got %s", text)
	}
}

func TestEditPagePreservesCRLF(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\r\n",
	})
	_ = serve(t, svc, call("edit_page", map[string]any{
		"route": "/", "find": "<h1>Hi</h1>", "replace": "<h1>Yo</h1>", "apply": true,
	}))
	data, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(data), "\r\n") {
		t.Fatalf("expected CRLF preserved, got %q", data)
	}
	if strings.Contains(string(data), "<h1>Hi</h1>") {
		t.Errorf("expected replacement applied, got %q", data)
	}
}

func TestReadContentListAndEntry(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":                 "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/blog/hello-world.md":     "---\ntitle: Hello World\n---\n# Hello\n\nBody text.\n",
		"src/content/blog/guides/advanced.md": "---\ntitle: Advanced\n---\nDeep body.\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "src/content/blog"}}

	text := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "nope"}))[0])
	if !strings.Contains(text, "no collection") {
		t.Fatalf("expected unknown-collection error, got %s", text)
	}

	listText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "blog"}))[0])
	var listing map[string]any
	if err := json.Unmarshal([]byte(listText), &listing); err != nil {
		t.Fatalf("listing is not JSON: %v\n%s", err, listText)
	}
	if listing["collection"] != "blog" {
		t.Errorf("collection = %v, want blog", listing["collection"])
	}
	entries := listing["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %v", entries)
	}
	first := entries[0].(map[string]any)
	if first["slug"] != "guides/advanced" {
		t.Errorf("first slug = %v, want guides/advanced", first["slug"])
	}
	if _, ok := first["data"].(map[string]any); !ok {
		t.Errorf("expected frontmatter in listing entry: %v", first)
	}

	entryText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "blog", "slug": "hello-world"}))[0])
	var entry map[string]any
	if err := json.Unmarshal([]byte(entryText), &entry); err != nil {
		t.Fatalf("entry is not JSON: %v\n%s", err, entryText)
	}
	if entry["slug"] != "hello-world" {
		t.Errorf("slug = %v, want hello-world", entry["slug"])
	}
	if !strings.Contains(entry["content"].(string), "Body text") {
		t.Errorf("expected raw content, got %v", entry["content"])
	}
	data := entry["data"].(map[string]any)
	if data["title"] != "Hello World" {
		t.Errorf("title = %v, want Hello World", data["title"])
	}
	if !strings.Contains(entry["body"].(string), "Body text") {
		t.Errorf("expected markdown body, got %v", entry["body"])
	}
	if _, ok := entry["html"].(string); !ok {
		t.Errorf("expected rendered html, got %v", entry["html"])
	}
}

func TestCreatePageTemplatesContentEntryAndLayout(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "src/content/blog"}}

	resps := serve(t, svc, call("create_page", map[string]any{
		"route":      "/blog",
		"template":   "content-list",
		"collection": "blog",
		"title":      "Blog",
		"contentEntry": map[string]any{
			"slug":  "hello-world",
			"title": "Hello World",
		},
		"withLayout": true,
		"apply":      true,
	}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, "Created") {
		t.Fatalf("expected success, got %s", text)
	}

	page, err := os.ReadFile(filepath.Join(svc.root, "src", "pages", "blog.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `getCollection("blog")`) {
		t.Errorf("expected collection glue in blog.tsx, got %s", page)
	}

	entry, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "blog", "hello-world.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "slug") && !strings.Contains(string(entry), "title: Hello World") {
		t.Errorf("unexpected entry content: %s", entry)
	}

	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "_layout.tsx")); err != nil {
		t.Errorf("withLayout should create _layout.tsx: %v", err)
	}
}

func TestConfigResourceShape(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"krate://config"}}`)
	contents := mustResp(t, resp[0])["contents"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	if strings.Contains(text, svc.root) {
		t.Errorf("config resource must not leak absolute paths: %s", text)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("config resource is not JSON: %v", err)
	}
	if v["pagesDir"] != "src/pages" {
		t.Errorf("pagesDir = %v, want src/pages", v["pagesDir"])
	}
	if _, isBool := v["minify"].(bool); !isBool {
		t.Errorf("minify should be a bool, got %v", v["minify"])
	}
}

// serveServer drives a pre-configured server (extra tools allowed) and returns
// decoded responses in order.
func TestCancellationQueuedRequest(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(&out, "krate", "test")
	slow := func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
		select {
		case <-ctx.Done():
			return ErrorResult("cancelled"), nil
		case <-time.After(300 * time.Millisecond):
			return TextResult("done"), nil
		}
	}
	srv.RegisterTool(Tool{Name: "slow", InputSchema: objSchema(nil), Handler: slow})
	srv.RegisterTool(Tool{Name: "fast", InputSchema: objSchema(nil), Handler: func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) { return TextResult("fast"), nil }})

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"notifications/cancelled","params":{"requestId":2,"reason":"test"}}`,
	}
	if err := srv.Serve(strings.NewReader(strings.Join(requests, "\n") + "\n")); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var responses []map[string]any
	dec := json.NewDecoder(&out)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d: %v", len(responses), responses)
	}
	if responses[0]["result"] == nil {
		t.Errorf("slow request should complete: %v", responses[0])
	}
	errObj, ok := responses[1]["error"].(map[string]any)
	if !ok {
		t.Fatalf("cancelled request should be an error: %v", responses[1])
	}
	if code, _ := errObj["code"].(float64); code != codeRequestCancelled {
		t.Errorf("error code = %v, want %d", code, codeRequestCancelled)
	}
}
