package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/frontmatter"
)

// contentTypeResult separates advisory failures (unreadable files or IO
// problems) from schema validation errors, which are real content bugs.
type contentTypeResult struct {
	Warnings   []error
	Validation []error
}

// contentConfig returns the typed content config from `krate.config.ts`
// (`content: defineContent({...})` or a plain object), or nil when none is
// declared.
func (b *Builder) contentConfig() *content.Config {
	return content.ParseConfig(b.Cfg.Content)
}

// writeContentTypes discovers each collection's entries, validates their
// frontmatter against the schema, and writes `.krate/types/content.d.ts`.
// Returns a zero result when no content collections are configured.
func (b *Builder) writeContentTypes() contentTypeResult {
	var res contentTypeResult

	cfg := b.contentConfig()
	if cfg == nil || len(cfg.Collections) == 0 {
		return res
	}

	entries := map[string][]content.Entry{}
	for name, col := range cfg.Collections {
		dir := col.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(b.Root, filepath.FromSlash(dir))
		}
		found, walkErrs := discoverContentEntries(b.Root, dir)
		res.Warnings = append(res.Warnings, walkErrs...)
		for _, e := range found {
			if schemaErrs := content.Validate(col.Schema, e.Data); len(schemaErrs) > 0 {
				for _, se := range schemaErrs {
					res.Validation = append(res.Validation, fmt.Errorf("%s: %s: %w", name, filepath.ToSlash(e.Path), se))
				}
			}
		}
		entries[name] = found
	}

	typesDir := filepath.Join(b.Root, filepath.FromSlash(routeTypesDir))
	if err := os.MkdirAll(typesDir, 0755); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("creating types dir: %w", err))
		return res
	}
	out := content.Generate(cfg, entries)
	if err := os.WriteFile(filepath.Join(typesDir, "content.d.ts"), []byte(out), 0644); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("writing content.d.ts: %w", err))
	}
	return res
}

// discoverContentEntries walks a collection dir for .md/.mdx files and parses
// each one's frontmatter.
func discoverContentEntries(root, dir string) ([]content.Entry, []error) {
	var entries []content.Entry
	var errs []error
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil // a declared collection with no dir yet is not an error
	}
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ext)
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			errs = append(errs, fmt.Errorf("reading %s: %w", path, rerr))
			return nil
		}
		data, _ := frontmatter.Parse(string(src))
		if data == nil {
			data = map[string]any{}
		}
		projRel := path
		if pr, rerr := filepath.Rel(root, path); rerr == nil {
			projRel = pr
		}
		entries = append(entries, content.Entry{
			Slug: slug,
			Path: filepath.ToSlash(projRel),
			Data: content.NormalizeData(data),
		})
		return nil
	})
	if walkErr != nil {
		errs = append(errs, fmt.Errorf("walking %s: %w", dir, walkErr))
	}
	return entries, errs
}
