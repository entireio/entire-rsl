package guard

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAPIs match calls that write refs, objects, config, the RSL or files.
var writeAPIs = []*regexp.Regexp{
	regexp.MustCompile(`\.(SetReference|DeleteReference|CheckAndSetReference|CheckAndDeleteReference|RemoveReference|SetEncodedObject|SetConfig|SetIndex|SetShallow)\(`),
	regexp.MustCompile(`\.(WriteBlob|WriteTree|Commit|CommitUsingSpecificKey|CommitWithoutNumber|ResetDueToError)\(`),
	regexp.MustCompile(`RecordRSL|RecordRSLAnnotation|SkipAllInvalidReferenceEntries|ApplyPolicy|StagePolicy|PushPolicy|PullPolicy|PushRSL|PullRSL|ReconcileLocalRSLWithRemote|\.Sync\(|policy\.Bootstrap\(|policy\.Author\(`),
	regexp.MustCompile(`\.(Push|Fetch|FetchRefSpec|FetchObject|CreateBranch|CreateTag|DeleteTag|DeleteBranch|DeleteRemote|CreateRemote|Worktree)\(`),
	regexp.MustCompile(`os\.(WriteFile|Create|CreateTemp|Mkdir|MkdirAll|MkdirTemp|Remove|RemoveAll|Rename|OpenFile|Chmod|Chtimes|Symlink|Link|Truncate)\(`),
}

// gitSubcommands match git subcommands that write. They only matter in files
// that start processes, where a string can become a git invocation.
var gitSubcommands = regexp.MustCompile(`"(update-ref|push|fetch|commit|commit-tree|tag|branch|gc|repack|prune|reset|checkout|config|remote|mktree|hash-object|write-tree|symbolic-ref|replace|notes|stash|merge|rebase|am|apply)"`)

// allowed lists, per file, the write APIs it may use. It is empty except for
// the fetch, entire-rsl's only write path.
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

type sourceLine struct {
	file string
	text string
	n    int
	// execs is set when the file imports os/exec.
	execs bool
}

// productionLines yields the code lines of every non-test Go file, comments
// and method declarations removed.
func productionLines(t *testing.T, root string) []sourceLine {
	t.Helper()

	var lines []sourceLine
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
		if !strings.HasSuffix(rel, ".go") || exempt(rel) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		var fileLines []sourceLine
		execs := false
		scanner := bufio.NewScanner(f)
		for n := 1; scanner.Scan(); n++ {
			text := strings.TrimSpace(scanner.Text())
			if text == `"os/exec"` {
				execs = true
			}
			if strings.HasPrefix(text, "//") || strings.HasPrefix(text, "func ") {
				continue
			}
			if i := strings.Index(text, " //"); i >= 0 {
				text = text[:i]
			}
			fileLines = append(fileLines, sourceLine{file: rel, n: n, text: text})
		}
		for i := range fileLines {
			fileLines[i].execs = execs
		}
		lines = append(lines, fileLines...)
		return scanner.Err()
	})
	require.NoError(t, err)
	return lines
}

func TestNoWritePathOutsideTheFetch(t *testing.T) {
	t.Parallel()

	for _, l := range productionLines(t, moduleRoot(t)) {
		patterns := writeAPIs
		if l.execs {
			patterns = append(slices.Clone(writeAPIs), gitSubcommands)
		}
		for _, re := range patterns {
			for _, match := range re.FindAllString(l.text, -1) {
				if isAllowed(l.file, match) {
					continue
				}
				assert.Failf(t, "write API outside the allowlist", "%s:%d uses %s: %s", l.file, l.n, match, l.text)
			}
		}
	}
}

func isAllowed(file, match string) bool {
	for _, a := range allowed[file] {
		if strings.Contains(match, a) {
			return true
		}
	}
	return false
}

func TestTestutilIsOnlyImportedByTests(t *testing.T) {
	t.Parallel()

	for _, l := range productionLines(t, moduleRoot(t)) {
		assert.NotContains(t, l.text, `"github.com/entireio/entire-rsl/internal/testutil"`, "%s:%d imports testutil", l.file, l.n)
	}
}

func TestGuardCatchesWrites(t *testing.T) {
	t.Parallel()

	for _, sample := range []string{
		`if err := st.SetReference(ref, id); err != nil {`,
		`rsl.NewReferenceEntry(ref, id).Commit(repo, true)`,
		`repo.RecordRSLEntryForReference(ctx, ref, true)`,
		`os.Chmod(path, 0o700)`,
		`os.WriteFile(path, data, 0o600)`,
		`_, err := policy.Bootstrap(ctx, store, params)`,
	} {
		hit := false
		for _, re := range writeAPIs {
			hit = hit || re.MatchString(sample)
		}
		assert.True(t, hit, sample)
	}
}

func TestGuardCatchesGitSubcommandsInProcessCode(t *testing.T) {
	t.Parallel()

	assert.True(t, gitSubcommands.MatchString(`args := []string{"update-ref", ref, id}`))
	assert.True(t, gitSubcommands.MatchString(`g.run(ctx, root, "push", remote)`))
}
