package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCalls match calls, rendered as "pkg.Func(" for package functions and
// ".Method(" for methods, that write refs, objects, config, the RSL or files.
var writeCalls = []*regexp.Regexp{
	regexp.MustCompile(`^\.(SetReference|DeleteReference|CheckAndSetReference|CheckAndDeleteReference|RemoveReference|SetEncodedObject|SetConfig|SetIndex|SetShallow)\($`),
	regexp.MustCompile(`^\.(WriteBlob|WriteTree|Commit|CommitUsingSpecificKey|CommitWithoutNumber|ResetDueToError)\($`),
	regexp.MustCompile(`^\.(RecordRSL\w*|SkipAllInvalidReferenceEntriesForRef|ApplyPolicy|StagePolicy|PushPolicy|PullPolicy|PushRSL|PullRSL|ReconcileLocalRSLWithRemote|Sync)\($`),
	regexp.MustCompile(`^(gittuf\.AuthorPolicy|rsl\.SkipAllInvalidReferenceEntriesForRef)\($`),
	regexp.MustCompile(`^\.(Push|Fetch|FetchRefSpec|FetchObject|CreateBranch|CreateTag|DeleteTag|DeleteBranch|DeleteRemote|CreateRemote|Worktree)\($`),
	regexp.MustCompile(`^os\.(WriteFile|Create|CreateTemp|Mkdir|MkdirAll|MkdirTemp|Remove|RemoveAll|Rename|OpenFile|Chmod|Chtimes|Symlink|Link|Truncate)\($`),
}

// gitSubcommands match string literals naming git subcommands that write.
// They only matter in files that start processes, where a string can become
// a git invocation.
var gitSubcommands = regexp.MustCompile(`^(update-ref|push|fetch|commit|commit-tree|tag|branch|gc|repack|prune|reset|checkout|config|remote|mktree|hash-object|write-tree|symbolic-ref|replace|notes|stash|merge|rebase|am|apply)$`)

// allowed lists, per file, the findings it may contain. It is empty except
// for the fetch, entire-rsl's only write path.
var allowed = map[string][]string{
	"internal/gitcli/fetch.go": {`"fetch"`, `"update-ref"`},
	// Worktree only locates the working tree root, it is never written.
	"internal/repo/repo.go": {".Worktree("},
}

// exempt files are not production code paths: testutil builds fixtures for
// tests and is checked separately below.
func exempt(rel string) bool {
	return strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "internal/testutil/")
}

type finding struct {
	file  string
	match string
	line  int
}

// scanFile reports the write calls in a Go source file, and the writing git
// subcommands named in it when it imports os/exec.
func scanFile(t *testing.T, path, rel string, src any) []finding {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	require.NoError(t, err)

	imports := map[string]bool{}
	execs := false
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		require.NoError(t, err)
		if p == "os/exec" {
			execs = true
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = true
	}

	var out []finding
	report := func(pos token.Pos, match string) {
		out = append(out, finding{file: rel, match: match, line: fset.Position(pos).Line})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if call := renderCall(n.Fun, imports); call != "" {
				for _, re := range writeCalls {
					if re.MatchString(call) {
						report(n.Pos(), call)
					}
				}
			}
		case *ast.BasicLit:
			if execs && n.Kind == token.STRING {
				if v, err := strconv.Unquote(n.Value); err == nil && gitSubcommands.MatchString(v) {
					report(n.Pos(), strconv.Quote(v))
				}
			}
		}
		return true
	})
	return out
}

// renderCall names a call's function as "pkg.Func(" or ".Method(".
func renderCall(fun ast.Expr, imports map[string]bool) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] {
		return id.Name + "." + sel.Sel.Name + "("
	}
	return "." + sel.Sel.Name + "("
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found")
		dir = parent
	}
}

// productionFiles lists every non-test Go file, relative to the module root.
func productionFiles(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "dist" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".go") && !exempt(rel) {
			files[rel] = path
		}
		return nil
	})
	require.NoError(t, err)
	return files
}

func TestNoWritePathOutsideTheFetch(t *testing.T) {
	t.Parallel()

	for rel, path := range productionFiles(t, moduleRoot(t)) {
		for _, f := range scanFile(t, path, rel, nil) {
			if !isAllowed(f) {
				assert.Failf(t, "write API outside the allowlist", "%s:%d uses %s", f.file, f.line, f.match)
			}
		}
	}
}

func isAllowed(f finding) bool {
	for _, a := range allowed[f.file] {
		if f.match == a {
			return true
		}
	}
	return false
}

func TestTestutilIsOnlyImportedByTests(t *testing.T) {
	t.Parallel()

	for rel, path := range productionFiles(t, moduleRoot(t)) {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			assert.NotEqual(t, `"github.com/entireio/entire-rsl/internal/testutil"`, imp.Path.Value, "%s imports testutil", rel)
		}
	}
}

func TestGuardCatchesWrites(t *testing.T) {
	t.Parallel()

	src := `package sample

import (
	"os"
	"os/exec"

	"github.com/gittuf/gittuf/experimental/gittuf"
	"github.com/gittuf/gittuf/pkg/rsl"
)

func oneLine() error { return os.WriteFile("x", nil, 0o600) }

func ref(st store) error { return st.SetReference("refs/heads/x", nil) }

func entry(st store) error {
	return rsl.NewReferenceEntry("refs/heads/x", nil).Commit(st, true)
}

func record(r repo) error { return r.RecordRSLEntryForReference(nil, "x", true) }

func author(s store) { gittuf.AuthorPolicy(nil, s, gittuf.AuthorPolicyParams{}) }

func run() { _ = exec.Command("git", "update-ref", "refs/heads/x", "y") }
`
	findings := scanFile(t, "sample.go", "internal/sample/sample.go", src)

	var matches []string
	for _, f := range findings {
		matches = append(matches, f.match)
	}
	assert.ElementsMatch(t, []string{"os.WriteFile(", ".SetReference(", ".Commit(", ".RecordRSLEntryForReference(", "gittuf.AuthorPolicy(", `"update-ref"`}, matches)
}

func TestGuardIgnoresStringsOutsideProcessCode(t *testing.T) {
	t.Parallel()

	src := `package sample

func label() string { return "branch" }
`
	assert.Empty(t, scanFile(t, "sample.go", "internal/sample/sample.go", src))
}
