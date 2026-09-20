// Package fsutil provides shared filesystem helpers used across the compiler.
package fsutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// WriteFileIfChanged writes data to path only when the existing contents
// differ, creating the file when absent. It prevents no-op writes from
// re-triggering file watchers (the dev loop rebuilds on source-tree writes,
// including generated declaration files rewritten identically every build).
func WriteFileIfChanged(path string, data []byte, perm os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return os.WriteFile(path, data, perm)
}

// WalkExt walks root and invokes fn for every regular file whose extension is
// in exts. Directories named in skipDirs (base name match) are pruned. The first
// error returned by fn or the walker is returned.
func WalkExt(root string, exts map[string]bool, skipDirs map[string]bool, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs != nil && skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if exts != nil && !exts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		return fn(path, info)
	})
}

// HasAnyExt reports whether path ends with one of the given extensions.
func HasAnyExt(path string, exts ...string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}
