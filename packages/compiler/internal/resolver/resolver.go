// Package resolver provides the shared node_modules package resolution used by
// the bundler (bare import specifiers) and the docs plugin (npm docs themes).
//
// Resolution mirrors Node's `module`/`main`/index fallback: a package's entry
// file is picked from package.json `module` (preferred) or `main`, then from a
// top-level index.* file, by walking up the directory tree from a start dir
// looking for node_modules/<pkg>.
package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PkgJSON is the subset of package.json the resolver reads to find a package
// entry file.
type PkgJSON struct {
	Main   string `json:"main"`
	Module string `json:"module"`
}

// Exists reports whether a file or directory exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// NodeModule resolves a bare npm package specifier ("formslib",
// "@scope/formslib") by walking up from startDir looking for node_modules/pkg.
// It returns the absolute path to the package entry file, or "" if the package
// (or an entry file) cannot be found. startDir may be an absolute or
// root-relative path.
func NodeModule(startDir, pkg string) string {
	dir := startDir
	for {
		pkgDir := pkg
		if strings.HasPrefix(pkg, "@") {
			s := strings.SplitN(pkg, "/", 3)
			if len(s) >= 2 {
				pkgDir = filepath.Join(s[0], s[1])
			}
		}
		base := filepath.Join(dir, "node_modules", pkgDir)
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			return PackageDir(base)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// NodeModuleDir resolves a bare npm specifier that may include a subpath,
// e.g. "formslib", "@scope/formslib", or "formslib/styles/x.css". For a bare
// package name it returns the package entry file (via PackageDir); when a
// subpath is present it returns the joined subpath. It walks up from startDir
// looking for node_modules/<pkg>, returning "" when nothing resolves.
func NodeModuleDir(startDir, spec string) string {
	pkg, sub := splitSpec(spec)
	if pkg == "" {
		return ""
	}
	dir := startDir
	for {
		base := filepath.Join(dir, "node_modules", pkg)
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			if sub == "" {
				return PackageDir(base)
			}
			candidate := filepath.Join(base, filepath.FromSlash(sub))
			if Exists(candidate) {
				return candidate
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// splitSpec separates a package specifier into its package name and optional
// subpath ("@scope/pkg/sub/file" -> "@scope/pkg", "sub/file").
func splitSpec(spec string) (pkg, sub string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}
	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 2 || parts[1] == "" {
			return "", ""
		}
		pkg = parts[0] + "/" + parts[1]
		if len(parts) == 3 {
			sub = parts[2]
		}
		return pkg, sub
	}
	parts := strings.SplitN(spec, "/", 2)
	if parts[0] == "" {
		return "", ""
	}
	if len(parts) == 2 {
		sub = parts[1]
	}
	return parts[0], sub
}

// PackageDir resolves a package directory's entry file: package.json `module`
// (preferred), then `main`, then a top-level index.* — or "" if none exist.
func PackageDir(pkgDir string) string {
	pkgJSONPath := filepath.Join(pkgDir, "package.json")
	if data, err := os.ReadFile(pkgJSONPath); err == nil {
		var pkg PkgJSON
		if json.Unmarshal(data, &pkg) == nil {
			if pkg.Module != "" {
				candidate := filepath.Join(pkgDir, pkg.Module)
				if Exists(candidate) {
					return candidate
				}
			}
			if pkg.Main != "" {
				candidate := filepath.Join(pkgDir, pkg.Main)
				if Exists(candidate) {
					return candidate
				}
			}
		}
	}

	indices := []string{"index.tsx", "index.ts", "index.jsx", "index.js"}
	for _, idx := range indices {
		candidate := filepath.Join(pkgDir, idx)
		if Exists(candidate) {
			return candidate
		}
	}

	return ""
}
