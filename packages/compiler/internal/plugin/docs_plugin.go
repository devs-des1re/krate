package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

type DocsPluginOptions struct {
	ContentDir string             `json:"contentDir"`
	Title      string             `json:"title"`
	Layout     string             `json:"layout"`
	Theme      json.RawMessage    `json:"theme"` // string (path or npm specifier) or DocsThemeDescriptor
	Sidebar    []docs.SidebarItem `json:"sidebar"`
	Links      []SocialLink       `json:"links"`
	Search     *DocsSearchOptions `json:"search"`
}

// DocsThemeDescriptor mirrors the shape a docs theme factory returns
// (@krate/plugin DocsThemeDescriptor). Module is an absolute filesystem path
// (a file:// URL converted to a path by the config bootstrap) or a npm/relative
// specifier; Options are forwarded to the layout component as props.options.
type DocsThemeDescriptor struct {
	Name    string                 `json:"name,omitempty"`
	Module  string                 `json:"module,omitempty"`
	Layout  string                 `json:"layout,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type SocialLink struct {
	Icon string `json:"icon"`
	URL  string `json:"url"`
}

func init() {
	Register(&DocsPlugin{})
}

type DocsPlugin struct{}

func (p *DocsPlugin) Name() string { return "docs" }
func (p *DocsPlugin) Order() int   { return 10 }

func (p *DocsPlugin) Hooks() PluginHooks {
	return PluginHooks{
		BeforeBuild: p.beforeBuild,
	}
}

func parseDocsOptions(cfg *config.Config) *DocsPluginOptions {
	for _, pc := range cfg.Plugins {
		if pc.Name == "docs" {
			opts := &DocsPluginOptions{
				ContentDir: "content/docs",
				Title:      "Docs",
			}
			if pc.Options != nil {
				data, err := json.Marshal(pc.Options)
				if err != nil {
					return nil
				}
				if err := json.Unmarshal(data, opts); err != nil {
					return nil
				}
			}
			return opts
		}
	}
	return nil
}

func (p *DocsPlugin) beforeBuild(ctx *BuildHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}

	opts := parseDocsOptions(cfg)
	if opts == nil {
		return nil
	}

	scanCfg := docs.Config{
		ContentDir: opts.ContentDir,
		Root:       ctx.Root,
		MDConfig:   cfg.Markdown,
	}

	pages, err := docs.Scan(scanCfg)
	if err != nil {
		return fmt.Errorf("scanning docs: %w", err)
	}
	if len(pages) == 0 {
		return nil
	}

	sections := docs.BuildSidebarTree(pages)

	p.writeAssets(ctx, sections, pages, opts)

	// Search bar + search index (docfind WASM, embedded in-process)
	searchEnabled, searchEngine, searchMaxResults := searchConfig(opts)
	if searchEnabled {
		if err := p.buildSearchAssets(ctx, pages, searchEngine, searchMaxResults); err != nil {
			fmt.Fprintf(os.Stderr, "  Docs search warning: %v (falling back to JSON search)\n", err)
		}
	}

	genDir := filepath.Join(ctx.Root, ".krate", "gen", "docs")

	os.RemoveAll(genDir)
	os.MkdirAll(genDir, 0755)

	// Resolve the docs layout/theme once up front. The result is either a bare
	// npm specifier (kept as-is so CSS/sub-components flow through the bundler
	// import graph) or an absolute layout file path (relativized per page).
	theme, err := p.resolveDocsTheme(ctx.Root, opts)
	if err != nil {
		return err
	}

	// Generate the SearchBar component once into the gen dir (shared by all pages)
	if searchEnabled {
		os.WriteFile(filepath.Join(genDir, "SearchBar.tsx"), []byte(generateSearchBarTSX()), 0644)
	}

	var themeOptions json.RawMessage
	if theme != nil {
		themeOptions = theme.options
	}

	type pageGenResult struct {
		tsxPath string
		route   string
	}

	resultsCh := make(chan pageGenResult, len(pages))
	var wg sync.WaitGroup

	for i, page := range pages {
		var prevTitle, prevLink, nextTitle, nextLink string
		if i > 0 {
			prevTitle = pages[i-1].Title
			prevLink = docs.PageURL(pages[i-1].Path)
		}
		if i < len(pages)-1 {
			nextTitle = pages[i+1].Title
			nextLink = docs.PageURL(pages[i+1].Path)
		}

		wg.Add(1)
		go func(page docs.Page, prevTitle, prevLink, nextTitle, nextLink string) {
			defer wg.Done()

			tocItems := docs.ExtractTOC(page.Content)
			breadcrumbs := docs.BuildBreadcrumbs(page.Path)

			tsxPath := filepath.Join(genDir, page.Path+".tsx")
			fileLayoutRel := theme.importSpecifier(filepath.Dir(tsxPath))
			var searchBarRel string
			if searchEnabled {
				searchBarRel = searchBarImportRel(tsxPath, genDir)
			}
			tsxSource := p.generateTSX(ctx, page, fileLayoutRel, searchBarRel, sections, tocItems, breadcrumbs, prevTitle, prevLink, nextTitle, nextLink, opts.Title, opts.Links, themeOptions, cfg.Markdown)

			os.MkdirAll(filepath.Dir(tsxPath), 0755)
			os.WriteFile(tsxPath, []byte(tsxSource), 0644)

			route := docs.NormalizePagePath(page.Path)
			if route == "" {
				route = "docs"
			} else {
				route = "docs/" + route
			}
			resultsCh <- pageGenResult{tsxPath: tsxPath, route: route}
		}(page, prevTitle, prevLink, nextTitle, nextLink)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	for res := range resultsCh {
		if ctx.GeneratedPages != nil {
			*ctx.GeneratedPages = append(*ctx.GeneratedPages, GeneratedPage{Path: res.tsxPath, Route: res.route})
		}
	}

	return nil
}

// trimComponentExt strips a component file extension so both the raw file and
// its extensionless form resolve to the same module.
func trimComponentExt(s string) string {
	s = strings.TrimSuffix(s, ".tsx")
	s = strings.TrimSuffix(s, ".ts")
	s = strings.TrimSuffix(s, ".jsx")
	s = strings.TrimSuffix(s, ".js")
	return s
}

func resolveLayoutImport(genDir, root, layout string) string {
	if layout == "" {
		return ""
	}
	layoutPath := filepath.Join(root, trimComponentExt(layout))
	rel, err := filepath.Rel(genDir, layoutPath)
	if err != nil {
		return layout
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// resolvedDocsTheme is the outcome of resolving the docs plugin's layout/theme
// option. Exactly one of module/spec is set:
//   - module: absolute layout file path — emit a relative import from each page.
//   - spec:   bare npm specifier — keep it as-is so the theme's CSS and
//     sub-components flow through the bundler's node_modules resolution.
type resolvedDocsTheme struct {
	module  string
	spec    string
	options json.RawMessage
}

// importSpecifier returns the import path used by a generated page at genDir to
// reach this theme's layout component.
func (t *resolvedDocsTheme) importSpecifier(genDir string) string {
	if t == nil {
		return ""
	}
	if t.spec != "" {
		return t.spec
	}
	if t.module == "" {
		return ""
	}
	rel, err := filepath.Rel(genDir, t.module)
	if err != nil {
		return filepath.ToSlash(t.module)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// isPathLike reports whether a specifier is a filesystem path (relative, or a
// bare npm package name with a relative/drive prefix) rather than a bare package.
func isPathLike(s string) bool {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") {
		return true
	}
	if filepath.IsAbs(s) {
		return true
	}
	if len(s) >= 2 && s[1] == ':' {
		return true
	}
	return false
}

// resolveLayoutFile maps a root-relative layout/theme specifier to an absolute,
// extension-free file path for conflict comparisons. Bare npm specifiers are
// not path-like and map to "".
func resolveLayoutFile(root, layout string) string {
	if layout == "" {
		return ""
	}
	if isPathLike(layout) && !filepath.IsAbs(layout) {
		return filepath.Join(root, trimComponentExt(layout))
	}
	if filepath.IsAbs(layout) {
		return filepath.Clean(trimComponentExt(layout))
	}
	return ""
}

// themeLayoutConflict reports whether the legacy root-relative `layout` option
// and a path-like theme path resolve to different components. The layout option
// is always treated as a root-relative path even when it lacks a leading "./",
// so its absolute file must be compared directly rather than via
// resolveLayoutFile (which treats non-path-like strings as bare specifiers).
func themeLayoutConflict(root, layout, themeName, themePath string) error {
	if layout == "" {
		return nil
	}
	themeFile := resolveLayoutFile(root, themePath)
	if themeFile == "" {
		return nil
	}
	if layoutFile := filepath.Join(root, trimComponentExt(layout)); layoutFile != themeFile {
		return fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", layout, themeName)
	}
	return nil
}

// resolveDocsTheme resolves the docs plugin's layout/theme options into an
// importable layout component. It honors both the legacy `layout` option and
// the `theme` alias:
//
//   - theme (""| nothing) + layout -> root-relative file, like today.
//   - theme "./path" (or "/abs")  -> alias of layout; error if both are set and
//     resolve to different components.
//   - theme "npm-pkg"             -> installed docs theme, emitted as a bare
//     specifier so its CSS/sub-components bundle through node_modules.
//   - theme { module, layout, options } -> theme factory descriptor; module may
//     be an absolute path (from a file:// URL), a root-relative path, or a bare
//     npm specifier. options are forwarded to the layout as props.options.
func (p *DocsPlugin) resolveDocsTheme(root string, opts *DocsPluginOptions) (*resolvedDocsTheme, error) {
	hasTheme := len(opts.Theme) > 0 && string(opts.Theme) != "null"
	if !hasTheme {
		if opts.Layout == "" {
			return nil, nil
		}
		return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(opts.Layout))}, nil
	}

	// String form: a component path (alias of layout) or an npm package name.
	var themeStr string
	if err := json.Unmarshal(opts.Theme, &themeStr); err == nil {
		if isPathLike(themeStr) {
			if err := themeLayoutConflict(root, opts.Layout, themeStr, themeStr); err != nil {
				return nil, err
			}
			return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(themeStr))}, nil
		}
		if opts.Layout != "" {
			return nil, fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", opts.Layout, themeStr)
		}
		if entry := resolver.NodeModule(root, themeStr); entry == "" {
			return nil, fmt.Errorf("docs plugin: theme package %q not found in node_modules (searched from %s)", themeStr, root)
		}
		return &resolvedDocsTheme{spec: themeStr}, nil
	}

	// Descriptor (factory) form.
	var desc DocsThemeDescriptor
	if err := json.Unmarshal(opts.Theme, &desc); err != nil {
		return nil, fmt.Errorf("docs plugin: theme must be a package name, a component path, or a theme descriptor object: %w", err)
	}

	mod := desc.Module
	if mod == "" {
		mod = desc.Layout
	}
	if mod == "" {
		return nil, fmt.Errorf("docs plugin: theme descriptor %q has no module or layout to import", desc.Name)
	}

	var options json.RawMessage
	if desc.Options != nil {
		if data, err := json.Marshal(desc.Options); err == nil {
			options = data
		}
	}

	if isPathLike(mod) {
		if err := themeLayoutConflict(root, opts.Layout, desc.Name, mod); err != nil {
			return nil, err
		}
		if filepath.IsAbs(mod) {
			return &resolvedDocsTheme{module: trimComponentExt(mod), options: options}, nil
		}
		return &resolvedDocsTheme{module: filepath.Join(root, trimComponentExt(mod)), options: options}, nil
	}

	// Bare npm specifier module.
	if opts.Layout != "" {
		return nil, fmt.Errorf("docs plugin: both layout (%q) and theme (%q) are set but resolve to different components; set only one", opts.Layout, desc.Name)
	}
	if entry := resolver.NodeModule(root, mod); entry == "" {
		return nil, fmt.Errorf("docs plugin: theme descriptor module %q not found in node_modules", mod)
	}
	return &resolvedDocsTheme{spec: mod, options: options}, nil
}

func (p *DocsPlugin) generateTSX(ctx *BuildHookCtx, page docs.Page, layoutRel, searchBarRel string, sections []docs.SidebarItem, tocItems []docs.TOCItem, breadcrumbs []docs.Breadcrumb, prevTitle, prevLink, nextTitle, nextLink, siteTitle string, socialLinks []SocialLink, themeOptions json.RawMessage, mdConfig markdown.Config) string {
	var sb strings.Builder
	sb.WriteString("// Auto-generated by krate docs plugin\n")

	var mdxImports []string
	var segments []markdown.MDXSegment
	if data, err := os.ReadFile(page.SourcePath); err == nil {
		src := string(data)
		mdxImports = markdown.ExtractImports(src)
		_, segments = markdown.ParseMDXSegments(src, mdConfig)
	}
	useCode := markdown.HasCodeSegments(segments)
	useAside := markdown.HasAsideSegments(segments)

	for _, imp := range mdxImports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	if useCode {
		sb.WriteString("import { Code } from \"@krate/components\";\n")
	}
	if useAside {
		sb.WriteString("import { Aside } from \"@krate/components\";\n")
	}

	if layoutRel != "" {
		sb.WriteString(fmt.Sprintf("import DocsLayout from \"%s\";\n", layoutRel))
	}
	if searchBarRel != "" {
		sb.WriteString(fmt.Sprintf("import SearchBar from \"%s\";\n", searchBarRel))
	}

	sb.WriteString("\n")

	sidebarItems := sections
	if len(page.CustomSidebar) > 0 {
		sidebarItems = page.CustomSidebar
	}
	enriched := docs.EnrichSidebarItems(sidebarItems, page.Path)
	sidebarJSON, _ := json.Marshal(enriched)
	tocJSON, _ := json.Marshal(tocItems)
	breadcrumbsJSON, _ := json.Marshal(breadcrumbs)
	socialJSON, _ := json.Marshal(socialLinks)
	pageTitleJSON, _ := json.Marshal(page.Title)
	siteTitleJSON, _ := json.Marshal(siteTitle)
	currentPathJSON, _ := json.Marshal(page.Path)

	sb.WriteString("export default function DocPage() {\n")
	sb.WriteString("  const docsProps = {\n")
	sb.WriteString("    pageTitle: ")
	sb.WriteString(string(pageTitleJSON))
	sb.WriteString(",\n")
	sb.WriteString("    siteTitle: ")
	sb.WriteString(string(siteTitleJSON))
	sb.WriteString(",\n")
	sb.WriteString("    sidebarItems: ")
	sb.WriteString(string(sidebarJSON))
	sb.WriteString(",\n")
	sb.WriteString("    tocItems: ")
	sb.WriteString(string(tocJSON))
	sb.WriteString(",\n")
	sb.WriteString("    breadcrumbs: ")
	sb.WriteString(string(breadcrumbsJSON))
	sb.WriteString(",\n")

	if prevTitle != "" {
		prevTitleJSON, _ := json.Marshal(prevTitle)
		sb.WriteString("    prevTitle: ")
		sb.WriteString(string(prevTitleJSON))
		sb.WriteString(",\n")
	}
	if prevLink != "" {
		prevLinkJSON, _ := json.Marshal(prevLink)
		sb.WriteString("    prevLink: ")
		sb.WriteString(string(prevLinkJSON))
		sb.WriteString(",\n")
	}
	if nextTitle != "" {
		nextTitleJSON, _ := json.Marshal(nextTitle)
		sb.WriteString("    nextTitle: ")
		sb.WriteString(string(nextTitleJSON))
		sb.WriteString(",\n")
	}
	if nextLink != "" {
		nextLinkJSON, _ := json.Marshal(nextLink)
		sb.WriteString("    nextLink: ")
		sb.WriteString(string(nextLinkJSON))
		sb.WriteString(",\n")
	}

	sb.WriteString("    socialLinks: ")
	sb.WriteString(string(socialJSON))
	sb.WriteString(",\n")

	sb.WriteString("    currentPath: ")
	sb.WriteString(string(currentPathJSON))
	sb.WriteString(",\n")

	if len(strings.TrimSpace(string(themeOptions))) > 2 {
		sb.WriteString("    options: ")
		sb.WriteString(string(themeOptions))
		sb.WriteString(",\n")
	}

	sb.WriteString("  };\n")
	sb.WriteString("  return (\n")
	sb.WriteString("    <>\n")
	if searchBarRel != "" {
		sb.WriteString("      <SearchBar />\n")
	}
	sb.WriteString("      <DocsLayout {...docsProps} >")

	if len(segments) > 0 {
		sb.WriteString("\n      <div class=\"md-content\">\n")
		for _, seg := range segments {
			if seg.HTML != "" {
				sb.WriteString("        <div dangerouslySetInnerHTML={{__html: `")
				sb.WriteString(escapeTemplateLit(seg.HTML))
				sb.WriteString("`}} />\n")
			}
			if seg.JSX != "" {
				sb.WriteString("        ")
				sb.WriteString(seg.JSX)
				sb.WriteString("\n")
			}
			if seg.Code != nil {
				sb.WriteString("        ")
				sb.WriteString(markdown.BuildCodeJSX(seg.Code.Lang, seg.Code.Code))
				sb.WriteString("\n")
			}
			if seg.Aside != nil {
				sb.WriteString("        ")
				sb.WriteString(markdown.BuildAsideJSX(seg.Aside))
				sb.WriteString("\n")
			}
		}
		sb.WriteString("      </div>\n")
	} else {
		content := page.Content
		sb.WriteString("<div class=\"md-content\" dangerouslySetInnerHTML={{__html: `")
		sb.WriteString(escapeTemplateLit(content))
		sb.WriteString("`}} />")
	}

	sb.WriteString("      </DocsLayout>\n")
	sb.WriteString("    </>\n")
	sb.WriteString("  );\n")
	sb.WriteString("}\n")

	return sb.String()
}

func escapeTemplateLit(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

func (p *DocsPlugin) writeAssets(ctx *BuildHookCtx, sections []docs.SidebarItem, pages []docs.Page, opts *DocsPluginOptions) {
	outDir := filepath.Join(ctx.OutDir, "docs")
	os.MkdirAll(filepath.Join(outDir, "data"), 0755)

	sidebarData, _ := json.Marshal(sections)
	os.WriteFile(filepath.Join(outDir, "data/sidebar.json"), sidebarData, 0644)

	searchData, _ := json.Marshal(docs.BuildSearchIndex(pages))
	os.WriteFile(filepath.Join(outDir, "data/search-index.json"), searchData, 0644)
}
