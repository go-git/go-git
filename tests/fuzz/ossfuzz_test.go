// Package fuzz_test asserts the rules the OSS-Fuzz build imposes on this
// repository's fuzz targets, which `go test -fuzz` does not.
//
// OSS-Fuzz strips each fuzz test file down to its target bodies and passes the
// package to compile_native_go_fuzzer, which resolves a target by grepping the
// package directory for the target name:
//
//	https://github.com/google/oss-fuzz/blob/master/projects/go-git/build.sh
//
// A name that does not grep uniquely either breaks that build or, worse, makes
// the harness drop the target and carry on. Both outcomes are invisible to a
// normal test run, so they are checked here.
package fuzz_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// namePrefix is the prefix Go gives every fuzz target. It is kept apart from
// the "func " it follows in a declaration because OSS-Fuzz finds fuzz test
// files by grepping every *_test.go file for that pair, and this file holds no
// targets of its own.
const namePrefix = "Fuzz"

type target struct {
	name string
	file string // File declaring the target, relative to the module root.
	dir  string // Package directory holding that file.
}

func TestTargetsAreDiscoverable(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	targets := findTargets(t, root)
	require.NotEmpty(t, targets, "no fuzz targets found under %s", root)

	for _, tg := range targets {
		t.Run(tg.name, func(t *testing.T) {
			t.Parallel()

			// compile_native_go_fuzzer resolves a target to a file with
			//
			//	grep -r -l --include='*.go' -s "<name>" <package dir>
			//
			// and the coverage build copies that path into $OUT. A second
			// match turns the copy into one of two sources onto a single
			// destination, which fails the build for the whole project.
			named := filesContaining(t, root, goFiles(t, root, tg.dir), tg.name)
			require.Equal(t, []string{tg.file}, named,
				"%s names the target %s. The OSS-Fuzz coverage build fails unless the name appears only in the file declaring it",
				strings.Join(without(named, tg.file), ", "), tg.name)

			// The harness then counts what grep reports for the declaration
			// itself. On anything other than a single match it prints
			// "Could not find the function" and moves on, leaving the target
			// out of the build without failing it.
			require.Equal(t, 1, declarations(t, root, named, tg.name),
				"%q matches more than one target, so OSS-Fuzz silently skips %s. Rename it so that no target name is a prefix of another",
				"func "+tg.name, tg.name)
		})
	}
}

func TestTargetNamesAreUnique(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	// compile_native_go_fuzzer writes every fuzzer to $OUT/<target name>, and
	// the package a target belongs to is no part of that name. Two targets
	// sharing a name leave one binary, so whichever OSS-Fuzz builds first is
	// replaced by the other and never fuzzed.
	declaredBy := make(map[string][]string)
	for _, tg := range findTargets(t, root) {
		declaredBy[tg.name] = append(declaredBy[tg.name], tg.file)
	}

	duplicates := make(map[string][]string)
	for name, files := range declaredBy {
		if len(files) > 1 {
			duplicates[name] = files
		}
	}
	require.Empty(t, duplicates,
		"each target listed here is declared in more than one package, and OSS-Fuzz keeps one binary per name. Qualify the names with the package they cover")
}

// findTargets collects every fuzz target declared under root.
func findTargets(t *testing.T, root string) []target {
	t.Helper()

	var targets []target
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		file := relative(t, root, path)
		for _, name := range declaredIn(t, path) {
			targets = append(targets, target{name: name, file: file, dir: filepath.Dir(file)})
		}
		return nil
	})
	require.NoError(t, err)

	return targets
}

// declaredIn returns the names of the fuzz targets declared in file.
func declaredIn(t *testing.T, file string) []string {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)

	var names []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, namePrefix) {
			continue
		}
		if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
			continue
		}
		if isFuzzParam(fn.Type.Params.List[0].Type) {
			names = append(names, fn.Name.Name)
		}
	}

	return names
}

// isFuzzParam reports whether expr is the *testing.F a fuzz target takes.
func isFuzzParam(expr ast.Expr) bool {
	ptr, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "F" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "testing"
}

// goFiles lists the Go files under dir, as `grep -r --include='*.go'` sees them.
func goFiles(t *testing.T, root, dir string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			files = append(files, relative(t, root, path))
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(files)

	return files
}

// filesContaining narrows files to those holding name anywhere in their text.
func filesContaining(t *testing.T, root string, files []string, name string) []string {
	t.Helper()

	var found []string
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(root, file))
		require.NoError(t, err)
		if strings.Contains(string(content), name) {
			found = append(found, file)
		}
	}

	return found
}

// declarations counts the target declarations grep attributes to name.
func declarations(t *testing.T, root string, files []string, name string) int {
	t.Helper()

	count := 0
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(root, file))
		require.NoError(t, err)
		for line := range strings.Lines(string(content)) {
			if strings.Contains(line, "func "+name) && strings.Contains(line, "testing.F") {
				count++
			}
		}
	}

	return count
}

// without drops keep from files, leaving the unexpected matches to report.
func without(files []string, keep string) []string {
	rest := make([]string, 0, len(files))
	for _, file := range files {
		if file != keep {
			rest = append(rest, file)
		}
	}

	return rest
}

// relative rewrites path so failures name files the way the repository does.
func relative(t *testing.T, root, path string) string {
	t.Helper()

	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)

	return filepath.ToSlash(rel)
}

// repoRoot walks up from the test's directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(".")
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above the test directory")
		dir = parent
	}
}
