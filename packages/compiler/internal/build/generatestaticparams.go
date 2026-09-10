package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/reactive"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
	"github.com/kratejs/krate/packages/compiler/internal/tsexec"
)

// injectStaticParams seeds the root page component with concrete dynamic-route
// params so a statically generated page ([id].tsx via generateStaticParams)
// can render them. Params are exposed to the component's SSR evaluation via:
//   - `params` — a JSON object (e.g. { params } destructuring → params.id)
//   - each param key — a direct binding (e.g. { id } destructuring → id)
//
// Only pure, signal-less root components are SSREval'd this way; interactive
// (client) roots keep their normal hydration-driven emission.
func injectStaticParams(tree *irtree.ComponentTree, params map[string]string) {
	root := tree.Root
	if root == nil || params == nil || len(params) == 0 {
		return
	}
	if root.Tier != irtree.TierStatic && root.Tier != irtree.TierServer {
		// Interactive roots render their params reactively via the served props
		// script; injecting static bindings here would conflict with hydration.
		return
	}
	bindings := make(map[string]string, len(params)+1)
	objStr := make([]string, 0, len(params))
	for k, v := range params {
		bindings[k] = v
		bv, _ := json.Marshal(v)
		objStr = append(objStr, fmt.Sprintf("%q:%s", k, bv))
	}
	bindings["params"] = "{" + strings.Join(objStr, ",") + "}"
	if len(bindings) > 0 {
		if len(root.SSREvalBindings) > 0 {
			for k, v := range root.SSREvalBindings {
				if _, ok := bindings[k]; !ok {
					bindings[k] = v
				}
			}
		}
		root.SSREvalBindings = bindings
		root.IsSSREval = true
	}
}

// extractParamNames extracts parameter names from a dynamic route filename.
// e.g. "video/[id].tsx" → ["id"], "user/[username]/posts/[postId].tsx" → ["username", "postId"]
func extractParamNames(pagePath, pagesDir string) []string {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	var params []string
	for _, part := range parts {
		cleaned := strings.TrimSuffix(part, filepath.Ext(part))
		if strings.HasPrefix(cleaned, "[") && strings.HasSuffix(cleaned, "]") {
			paramName := cleaned[1 : len(cleaned)-1]
			params = append(params, paramName)
		}
	}
	return params
}

// hasGenerateStaticParams checks if the program exports a generateStaticParams function.
func hasGenerateStaticParams(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		fn, ok := exp.Declaration.(*ast.FnDecl)
		if ok && fn.Name == "generateStaticParams" {
			return true
		}
	}
	return false
}

// executeGenerateStaticParams runs the page's generateStaticParams via npx tsx
// and returns the parsed param combinations.
func executeGenerateStaticParams(pagePath string) ([]map[string]string, error) {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return nil, err
	}

	content := fmt.Sprintf(
		`import * as mod from '%s';
const fn = mod.generateStaticParams || (mod.default && mod.default.generateStaticParams);
if (typeof fn !== 'function') {
  process.stderr.write('generateStaticParams is not a function\n');
  process.exit(1);
}
const result = await fn();
console.log(JSON.stringify(result));
`,
		tsexec.ImportPath(abs),
	)

	output, _, err := tsexec.RunBootstrap("krate-gsp-bootstrap", content, filepath.Dir(abs), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("generateStaticParams execution: %w", err)
	}

	var paramSets []map[string]string
	if err := json.Unmarshal(output, &paramSets); err != nil {
		// Try as array of interfaces
		var rawSets []map[string]interface{}
		if err2 := json.Unmarshal(output, &rawSets); err2 != nil {
			return nil, fmt.Errorf("parsing generateStaticParams output: %w\nraw: %s", err, string(output))
		}
		for _, raw := range rawSets {
			ps := make(map[string]string)
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					ps[k] = val
				default:
					b, _ := json.Marshal(val)
					ps[k] = string(b)
				}
			}
			paramSets = append(paramSets, ps)
		}
	}

	return paramSets, nil
}

// staticParamsPage represents a page with params for static generation.
type staticParamsPage struct {
	PagePath string
	Params   map[string]string
	OutPath  string // e.g. "video/abc123"
}

// isDynamicRoute checks if a page path contains [param] segments.
func isDynamicRoute(pagePath, pagesDir string) bool {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return strings.Contains(rel, "[") && strings.Contains(rel, "]")
}

// resolveStaticParamsPages expands dynamic routes using generateStaticParams.
// It returns additional pages to build, each with concrete param values, plus
// any errors encountered while resolving dynamic routes (bundling or executing
// generateStaticParams). A dynamic route that lacks generateStaticParams is
// left to runtime resolution and is not an error.
func (b *Builder) resolveStaticParamsPages(pages []string) ([]staticParamsPage, error) {
	var expanded []staticParamsPage
	var errs []string

	for _, page := range pages {
		if !isDynamicRoute(page, b.Cfg.PagesDir) {
			continue
		}

		// Bundle the page to check for generateStaticParams
		bnd := bundler.New(b.Root)
		bnd.SetEmitReact(b.Cfg.EmitReact)
		bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
		bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)

		bundle, err := bnd.Bundle(page)
		if err != nil {
			errs = append(errs, fmt.Sprintf("failed to bundle %s for generateStaticParams: %v", page, err))
			continue
		}

		entryModule := findEntryModule(bundle.Modules)
		if entryModule == nil || entryModule.Program == nil {
			continue
		}

		if !hasGenerateStaticParams(entryModule.Program) {
			continue
		}

		fmt.Printf("  %s⚡%s generateStaticParams: %s\n", cCyan, cReset, filepath.Base(page))

		paramSets, err := executeGenerateStaticParams(page)
		if err != nil {
			errs = append(errs, fmt.Sprintf("generateStaticParams (%s): %v", filepath.Base(page), err))
			continue
		}

		for _, params := range paramSets {
			rel, _ := filepath.Rel(b.Cfg.PagesDir, page)
			rel = filepath.ToSlash(rel)
			rel = strings.TrimSuffix(rel, filepath.Ext(rel))

			parts := strings.Split(rel, "/")
			for i, part := range parts {
				if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
					paramName := part[1 : len(part)-1]
					if val, ok := params[paramName]; ok {
						parts[i] = val
					}
				}
			}
			outPath := strings.Join(parts, "/")

			expanded = append(expanded, staticParamsPage{
				PagePath: page,
				Params:   params,
				OutPath:  outPath,
			})
		}
	}

	if len(errs) > 0 {
		return expanded, fmt.Errorf("generateStaticParams resolution failed:\n  %s", strings.Join(errs, "\n  "))
	}

	return expanded, nil
}

// buildStaticParamsPage builds a single page with its params injected into the
// page component's props (via injectStaticParams).
func (b *Builder) buildStaticParamsPage(spp staticParamsPage) (*PageResult, string, error) {
	bnd := bundler.New(b.Root)
	bnd.SetEmitReact(b.Cfg.EmitReact)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)

	bundle, err := bnd.Bundle(spp.PagePath)
	if err != nil {
		return nil, "", err
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("no entry module found")
	}

	// Run AfterParse hooks so statically generated dynamic-param pages get the
	// same AST-inspection/edit opportunity as regular pages.
	parseCtx := &plugin.ParseHookCtx{
		Page:    spp.PagePath,
		Program: entryModule.Program,
	}
	if err := plugin.RunAfterParse(parseCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAfterParse plugin error (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterParse (%s): %v", spp.PagePath, err))
	}
	if err := plugin.RunCommunityPlugins("AfterParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, parseCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterParse (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterParse (%s): %v", spp.PagePath, err))
	}
	entryModule.Program = parseCtx.Program

	renderMode, revalidate := detectRenderMode(entryModule.Program)

	// Global streaming override: if configured, all *static* pages stream.
	// Explicit ssr/isr opts win — forcing them to streaming would silently
	// defeat a page author's per-page config.
	if b.Cfg.SSR.Streaming && renderMode == RenderSSG {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → streaming (global override)\n", cCyan, cReset, filepath.Base(spp.PagePath))
	}

	b.TransformUniversalIcons(entryModule.Program)
	b.TransformUniversalImages(entryModule.Program)

	// ─── New pipeline: Annotate → Build IR → Emit ──────────────────────────
	ann := annotator.Annotate(entryModule.Program, b.Cfg, spp.PagePath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	annotator.MergeImportAliases(ann, extraPrograms, annotator.ModuleSource{Program: entryModule.Program, Path: entryModule.Path, RawSource: entryModule.SourceCode})
	tree := irtree.Build(entryModule.Program, ann)
	injectStaticParams(tree, spp.Params)
	emitter := renderer.NewEmitter()
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	if len(emitResult.Errors) > 0 {
		return nil, "", renderErrors(spp.PagePath, emitResult.Errors)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	// Run AfterRender hooks (pre-layout) so plugins can modify HTML/head/CSS for
	// statically generated dynamic-param pages, matching buildPage.
	renderCtx := &plugin.RenderHookCtx{
		Page:     spp.PagePath,
		HTML:     emitResult.HTML,
		HeadHTML: emitResult.HeadHTML,
		HasJS:    len(emitResult.Signatures) > 0,
		RawCSS:   bundle.CSS,
	}
	if err := plugin.RunAfterRender(renderCtx); err != nil {
		fmt.Fprintf(os.Stderr, "  %sAfterRender plugin error (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterRender (%s): %v", spp.PagePath, err))
	}
	if err := plugin.RunCommunityPlugins("AfterRender", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, renderCtx, b.communityEnv()); err != nil {
		fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterRender (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
		b.pluginFailed(fmt.Errorf("AfterRender (%s): %v", spp.PagePath, err))
	}
	emitResult.HTML = renderCtx.HTML
	emitResult.HeadHTML = renderCtx.HeadHTML
	bundle.CSS = renderCtx.RawCSS

	// Run AfterMarkdownParse hooks for markdown/MDX dynamic-param pages.
	if strings.HasSuffix(spp.PagePath, ".md") || strings.HasSuffix(spp.PagePath, ".mdx") {
		mdCtx := &plugin.MarkdownHookCtx{
			Page:  spp.PagePath,
			HTML:  emitResult.HTML,
			Route: spp.OutPath,
		}
		if err := plugin.RunAfterMarkdownParse(mdCtx); err != nil {
			fmt.Fprintf(os.Stderr, "  %sAfterMarkdownParse plugin error (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
			b.pluginFailed(fmt.Errorf("AfterMarkdownParse (%s): %v", spp.PagePath, err))
		}
		if err := plugin.RunCommunityPlugins("AfterMarkdownParse", b.Cfg.Plugins, b.Root, b.Cfg.OutDir, mdCtx, b.communityEnv()); err != nil {
			fmt.Fprintf(os.Stderr, "  %sCommunity plugin error AfterMarkdownParse (%s):%s %v\n", cYellow, spp.PagePath, cReset, err)
			b.pluginFailed(fmt.Errorf("AfterMarkdownParse (%s): %v", spp.PagePath, err))
		}
		emitResult.HTML = mdCtx.HTML
	}

	layoutPath := findLayout(spp.PagePath, b.Cfg.PagesDir)
	if layoutPath != "" {
		bundle.CSS += b.applyLayoutStack(spp.PagePath, emitResult)
	}

	pageDir := filepath.Join(b.Cfg.OutDir, spp.OutPath)
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating page dir: %w", err)
	}

	jsFile := ""
	hydrationJS := ""
	hasJS := false
	needsHydrate := len(emitResult.Signatures) > 0
	if needsHydrate {
		hydrationJS = renderer.GenerateNewHydrationJS(emitResult)
		if strings.TrimSpace(hydrationJS) != "" {
			hasJS = true
			if b.Cfg.ShouldMinifyJS() {
				hydrationJS = minifyJS(hydrationJS)
			}
			finalJS := strings.TrimSpace(hydrationJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			finalJS = substituteImportMetaURL(finalJS, spp.OutPath, jsFile)
			jsPath := filepath.Join(pageDir, jsFile)
			os.WriteFile(jsPath, []byte(finalJS), 0644)
			hydrationJS = finalJS
		} else {
			hydrationJS = ""
		}
	}

	relSrc, _ := filepath.Rel(b.Root, spp.PagePath)
	if err := b.writeAssetFiles(bundle.AssetFiles); err != nil {
		return nil, "", fmt.Errorf("writing assets for %s: %w", spp.PagePath, err)
	}
	b.registerWorkers(bundle.WorkerFiles, bundle.WorkerEsm)
	b.registerDynamicChunks(bundle.DynImportFiles)
	return &PageResult{
		Page:        spp.PagePath,
		OutName:     spp.OutPath,
		HTML:        emitResult.HTML,
		HeadHTML:    emitResult.HeadHTML,
		ScriptHTML:  emitResult.ScriptHTML,
		StyleHTML:   emitResult.StyleHTML,
		HydrationJS: hydrationJS,
		HasJS:       hasJS,
		JSFile:      jsFile,
		HasCSS:      bundle.CSS != "",
		CSS:         bundle.CSS,
		UsedCSS:     emitResult.UsedCSS,
		UsedFuncs:   emitResult.UsedFuncs,
		Mode:        renderMode,
		Revalidate:  revalidate,
		SourcePath:  relSrc,
	}, bundle.CSS, nil
}
