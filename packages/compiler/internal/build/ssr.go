package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// SSRServer manages the SSR sidecar renderer process (node/bun/deno).
type SSRServer struct {
	port     int
	root     string
	runtime  string // "node" (default) | "bun" | "deno"
	cmd      *exec.Cmd
	mu       sync.Mutex
	running  bool
	manifest *ServerManifest
}

// NewSSRServer creates a new SSR server manager. runtime selects the sidecar
// runtime: "node" (default), "bun", or "deno".
func NewSSRServer(root string, port int, runtime string) *SSRServer {
	if runtime == "" {
		runtime = "node"
	}
	return &SSRServer{
		port:    port,
		root:    root,
		runtime: runtime,
	}
}

// Start launches the SSR renderer server process under the configured runtime
// (node | bun | deno). Prefers the staged driver (.krate/server-renderer.mjs)
// bundled at build time so plain runtimes work without tsx.
func (s *SSRServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	rendererPath := s.findRendererScript()
	if rendererPath == "" {
		return fmt.Errorf("krate SSR renderer not found — ensure @krate/runtime is installed")
	}

	manifestPath := filepath.Join(s.root, "dist", "server-manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("server-manifest.json not found in dist/ — no SSR/ISR pages to render")
	}

	// Load manifest for route lookup
	data, _ := os.ReadFile(manifestPath)
	s.manifest = &ServerManifest{}
	json.Unmarshal(data, s.manifest)

	runtimeCmd, runtimeArgs, err := ssrRuntimeCommand(s.runtime, rendererPath)
	if err != nil {
		return err
	}

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("KRATE_SSR_PORT=%d", s.port),
		fmt.Sprintf("KRATE_MANIFEST=%s", manifestPath),
		fmt.Sprintf("KRATE_ROOT=%s", s.root),
	)

	s.cmd = exec.Command(runtimeCmd, runtimeArgs...)
	s.cmd.Env = env
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("starting SSR renderer: %w", err)
	}

	s.running = true

	// Wait for server to be ready
	go func() {
		s.cmd.Wait()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	// Poll for readiness
	ready := false
	for i := 0; i < 50; i++ { // 5 seconds max
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/__krate/ssr/health", s.port))
		if err == nil && resp.StatusCode == 200 {
			ready = true
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if !ready {
		s.Stop()
		return fmt.Errorf("SSR renderer did not become ready within 5 seconds")
	}

	return nil
}

// Stop gracefully shuts down the Node.js renderer server.
func (s *SSRServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	s.running = false
}

// IsRunning returns whether the SSR server process is alive.
func (s *SSRServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// RevalidatePage triggers ISR revalidation for a specific route.
func (s *SSRServer) RevalidatePage(route string) error {
	if !s.IsRunning() {
		return fmt.Errorf("SSR renderer not running")
	}

	body, _ := json.Marshal(map[string]string{"route": route})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/__krate/ssr/revalidate", s.port),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// IsSSRPage checks if a route needs server-side rendering based on the manifest.
func (s *SSRServer) IsSSRPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "ssr" || page.Mode == "streaming"
	}
	return false
}

// IsISRPage checks if a route uses ISR.
func (s *SSRServer) IsISRPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "isr"
	}
	return false
}

// IsStreamingPage checks if a route uses streaming SSR.
func (s *SSRServer) IsStreamingPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "streaming"
	}
	return false
}

// GetRevalidate returns the ISR revalidation interval (seconds) for a route,
// falling back to the default when the page has no explicit value. Returns 0
// for non-ISR routes (callers shouldn't emit ISR cache headers then).
func (s *SSRServer) GetRevalidate(route string) int {
	page, _ := s.FindPageForRoute(route)
	if page == nil {
		return 0
	}
	if page.Revalidate > 0 {
		return page.Revalidate
	}
	return defaultISRRevalidate
}

// ISRPage represents an ISR page with its revalidation interval.
type ISRPage struct {
	Route      string
	Revalidate int // seconds
}

// GetISRPages returns all ISR pages and their revalidation intervals.
func (s *SSRServer) GetISRPages() []ISRPage {
	if s.manifest == nil {
		return nil
	}
	var pages []ISRPage
	for _, p := range s.manifest.Pages {
		if p.Mode == "isr" {
			reval := p.Revalidate
			if reval <= 0 {
				reval = 60
			}
			pages = append(pages, ISRPage{
				Route:      p.Route,
				Revalidate: reval,
			})
		}
	}
	return pages
}

// findPage returns the ManifestPage for a given route, or nil if not found.
func (s *SSRServer) findPage(route string) *ManifestPage {
	if s.manifest == nil {
		return nil
	}
	for i := range s.manifest.Pages {
		if s.manifest.Pages[i].Route == route {
			return &s.manifest.Pages[i]
		}
	}
	return nil
}

// FindPageForRoute matches a URL path against route patterns (including [param] segments)
// and returns the matching page with extracted params. Returns nil if no match.
func (s *SSRServer) FindPageForRoute(urlPath string) (*ManifestPage, map[string]string) {
	if s.manifest == nil {
		return nil, nil
	}
	for i := range s.manifest.Pages {
		p := &s.manifest.Pages[i]
		if params, ok := matchRoute(urlPath, p.Route); ok {
			return p, params
		}
	}
	return nil, nil
}

func (s *SSRServer) findRendererScript() string {
	// Prefer the bundled driver staged into dist by the build (runs with plain
	// node, no npx/tsx dependency at serve time).
	staged := filepath.Join(s.root, "dist", ".krate", "server-renderer.mjs")
	if _, err := os.Stat(staged); err == nil {
		abs, _ := filepath.Abs(staged)
		return abs
	}
	return findServerRendererSource(s.root)
}

// ssrRuntimeCommand returns the exec command + args that launch the SSR
// renderer script under the configured runtime. bun and deno get the flags the
// renderer needs (network to listen, filesystem to read dist + the ISR cache).
// When the script is raw TypeScript source (no staged driver), node runs it via
// tsx (legacy dev path).
func ssrRuntimeCommand(runtime, rendererPath string) (string, []string, error) {
	isTS := strings.HasSuffix(strings.ToLower(rendererPath), ".ts")
	switch runtime {
	case "", "node":
		if isTS {
			return "npx", []string{"tsx", rendererPath}, nil
		}
		return "node", []string{rendererPath}, nil
	case "bun":
		return "bun", append([]string{"run"}, rendererPath), nil
	case "deno":
		flags := []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys"}
		return "deno", append(flags, rendererPath), nil
	default:
		return "", nil, fmt.Errorf("unsupported ssrRuntime %q (want node, bun, or deno)", runtime)
	}
}

// findServerRendererSource locates the server-renderer source file in the
// @krate/runtime package, searching the monorepo and install layouts.
func findServerRendererSource(root string) string {
	// Search for the server-renderer file in the runtime package
	candidates := []string{
		// Monorepo: packages/runtime/src/server-renderer.ts
		filepath.Join(root, "packages", "runtime", "src", "server-renderer.ts"),
		// Monorepo from compiler dir
		filepath.Join(root, "..", "runtime", "src", "server-renderer.ts"),
		// npm installed
		filepath.Join(root, "node_modules", "@krate", "runtime", "src", "server-renderer.ts"),
		// Go binary relative
		filepath.Join(filepath.Dir(os.Args[0]), "..", "runtime", "src", "server-renderer.ts"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}

	// Walk up looking for packages/runtime
	dir := root
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "packages", "runtime", "src", "server-renderer.ts")
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// stageServerRenderer bundles the @krate/runtime server-renderer entrypoint into
// a standalone node-runnable ESM driver at <outDir>/.krate/server-renderer.mjs.
// The driver bundles the SSR runtime (server.ts) so `krate serve` only needs a
// plain `node <driver>` process instead of `npx tsx <source>.ts`. Returns the
// absolute staged path, or "" if the source couldn't be located or bundled.
func stageServerRenderer(root, outDir string) string {
	source := findServerRendererSource(root)
	if source == "" {
		return ""
	}

	stageDir := filepath.Join(outDir, ".krate")
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return ""
	}
	outPath := filepath.Join(stageDir, "server-renderer.mjs")

	result := api.Build(api.BuildOptions{
		AbsWorkingDir: root,
		Bundle:        true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformNode,
		Outfile:       outPath,
		Write:         true,
		LogLevel:      api.LogLevelSilent,
		Stdin: &api.StdinOptions{
			Loader:     api.LoaderTS,
			Contents:   readFileString(source),
			ResolveDir: filepath.Dir(source),
			Sourcefile: filepath.Base(source),
		},
	})

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  %sSSR renderer bundle error:%s %s\n", cYellow, cReset, e.Text)
		}
		return ""
	}

	abs, _ := filepath.Abs(outPath)
	return abs
}

func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func matchRoute(urlPath, pattern string) (map[string]string, bool) {
	urlParts := strings.Split(strings.Trim(urlPath, "/"), "/")
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")

	if len(urlParts) != len(patParts) {
		return nil, false
	}

	params := make(map[string]string)
	for i, pp := range patParts {
		if strings.HasPrefix(pp, "[") && strings.HasSuffix(pp, "]") {
			paramName := pp[1 : len(pp)-1]
			params[paramName] = urlParts[i]
		} else if pp != urlParts[i] {
			return nil, false
		}
	}

	return params, true
}
