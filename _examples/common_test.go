package examples

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v6/internal/test/gitenv"
)

var examplesTest = flag.Bool("examples", false, "run the examples tests")

var defaultURL = "https://github.com/git-fixtures/basic.git"

var args = map[string][]string{
	"blame":                      {cloneRepository(defaultURL, tempFolder()), "CHANGELOG"},
	"branch":                     {defaultURL, tempFolder()},
	"checkout":                   {defaultURL, tempFolder(), "35e85108805c84807bc66a02d91535e1e24b38b9"},
	"checkout-branch":            {defaultURL, tempFolder(), "branch"},
	"clone":                      {defaultURL, tempFolder()},
	"clone-commit-push":          {createBareRepositoryFrom(defaultURL, tempFolder()), tempFolder(), "example-branch"},
	"config":                     {},
	"commit":                     {cloneRepository(defaultURL, tempFolder())},
	"context":                    {defaultURL, tempFolder()},
	"custom_http":                {defaultURL},
	"find-if-any-tag-point-head": {cloneRepository(defaultURL, tempFolder())},
	"ls":                         {cloneRepository(defaultURL, tempFolder()), "HEAD", "vendor"},
	"ls-remote":                  {defaultURL},
	"memory":                     {defaultURL},
	"merge_base":                 {cloneRepository(defaultURL, tempFolder()), "--is-ancestor", "HEAD~3", "HEAD^"},
	"open":                       {cloneRepository(defaultURL, tempFolder())},
	"perf-clone":                 {cloneRepository(defaultURL, tempFolder())},
	"progress":                   {defaultURL, tempFolder()},
	"pull":                       {createRepositoryWithRemote(tempFolder(), defaultURL)},
	"push":                       {setEmptyRemote(cloneRepository(defaultURL, tempFolder()))},
	"restore":                    {cloneRepository(defaultURL, tempFolder())},
	"revision":                   {cloneRepository(defaultURL, tempFolder()), "master~2^"},
	"sha256":                     {tempFolder()},
	"showcase":                   {defaultURL, tempFolder()},
	"sparse-checkout":            {defaultURL, "vendor", tempFolder()},
	"tag":                        {cloneRepository(defaultURL, tempFolder())},
	"worktrees":                  {cloneRepository(defaultURL, tempFolder()), tempFolder()},
}

// tests not working / set-up
var ignored = map[string]bool{
	"ls":              true,
	"sha256":          true,
	"submodule":       true,
	"tag-create-push": true,
	"http-server":     true,
}

// verify holds optional post-run checks, keyed by example name. The runner only
// learns whether an example exited cleanly, which says nothing about an example
// whose point is the state it leaves in another repository.
var verify = map[string]func(*testing.T, []string){
	"clone-commit-push": verifyClonePushedBranch,
}

var (
	tempFolders = []string{}

	_, callingFile, _, _ = runtime.Caller(0)
	basepath             = filepath.Dir(callingFile)
)

func TestExamples(t *testing.T) {
	flag.Parse()
	if !*examplesTest && os.Getenv("CI") == "" {
		t.Skip("skipping examples tests, pass --examples to execute it")
		return
	}

	defer deleteTempFolders()

	exampleMains, err := filepath.Glob(filepath.Join(basepath, "*", "main.go"))
	if err != nil {
		t.Errorf("error finding tests: %s", err)
	}

	for _, main := range exampleMains {
		dir := filepath.Dir(main)
		_, name := filepath.Split(dir)

		if ignored[name] {
			continue
		}

		t.Run(name, func(t *testing.T) {
			testExample(t, name, dir)

			if check := verify[name]; check != nil && !t.Failed() {
				check(t, args[name])
			}
		})
	}
}

func tempFolder() string {
	path, err := os.MkdirTemp("", "")
	CheckIfError(err)

	tempFolders = append(tempFolders, path)
	return path
}

func cloneRepository(url, folder string) string {
	cmd := gitenv.Command("git", "clone", url, folder)
	err := cmd.Run()
	CheckIfError(err)

	return folder
}

func createBareRepository(dir string) string {
	return createRepository(dir, true)
}

func createBareRepositoryFrom(url, dir string) string {
	cmd := gitenv.Command("git", "clone", "--bare", url, dir)
	err := cmd.Run()
	CheckIfError(err)

	return dir
}

func createRepository(dir string, isBare bool) string {
	var cmd *exec.Cmd
	if isBare {
		cmd = gitenv.Command("git", "init", "--bare", dir)
	} else {
		cmd = gitenv.Command("git", "init", dir)
	}
	err := cmd.Run()
	CheckIfError(err)

	return dir
}

func createRepositoryWithRemote(local, remote string) string {
	createRepository(local, false)
	addRemote(local, remote)
	return local
}

func setEmptyRemote(dir string) string {
	remote := createBareRepository(tempFolder())
	setRemote(dir, remote)
	return dir
}

func setRemote(local, remote string) {
	cmd := gitenv.Command("git", "remote", "set-url", "origin", remote)
	cmd.Dir = local
	err := cmd.Run()
	CheckIfError(err)
}

func addRemote(local, remote string) {
	cmd := gitenv.Command("git", "remote", "add", "origin", remote)
	cmd.Dir = local
	err := cmd.Run()
	CheckIfError(err)
}

func testExample(t *testing.T, name, dir string) {
	arguments := append([]string{"run", dir}, args[name]...)
	cmd := exec.Command("go", arguments...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Errorf("error running cmd %q", err)
	}
}

func deleteTempFolders() {
	for _, folder := range tempFolders {
		err := os.RemoveAll(folder)
		CheckIfError(err)
	}
}

// verifyClonePushedBranch inspects the bare repository that clone-commit-push
// pushed to. Its arguments are the remote, the clone directory and the branch.
func verifyClonePushedBranch(t *testing.T, args []string) {
	remote, branch := args[0], args[2]
	ref := "refs/heads/" + branch

	// The fixture arrives with branch and master. A push scoped to the new
	// branch adds that one name and disturbs nothing else; the wildcard default
	// refspec would also carry any other local branch.
	got := gitLines(t, remote, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	want := []string{"branch", branch, "master"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("remote branches = %v, want %v", got, want)
	}

	if files := gitLines(t, remote, "ls-tree", "--name-only", ref); !slices.Contains(files, "example-git-file") {
		t.Errorf("pushed tree = %v, want it to contain example-git-file", files)
	}

	// The example branches from HEAD and adds a single commit, so the pushed
	// tip's parent is the commit master still points at. This pins both that the
	// commit travelled and that it was built on the cloned history.
	parent := gitLines(t, remote, "rev-parse", ref+"^")
	master := gitLines(t, remote, "rev-parse", "refs/heads/master")
	if parent[0] != master[0] {
		t.Errorf("pushed commit parent = %s, want master at %s", parent[0], master[0])
	}
}

func gitLines(t *testing.T, dir string, arg ...string) []string {
	cmd := gitenv.Command("git", append([]string{"-C", dir}, arg...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", arg, dir, err)
	}

	return strings.Split(strings.TrimSpace(string(out)), "\n")
}
