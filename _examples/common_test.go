package examples

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var examplesTest = flag.Bool("examples", false, "run the examples tests")

var defaultURL = "https://github.com/git-fixtures/basic.git"

var args = map[string][]string{
	"blame":                      {defaultURL, "CHANGELOG"},
	"branch":                     {defaultURL, tempFolder()},
	"checkout":                   {defaultURL, tempFolder(), "35e85108805c84807bc66a02d91535e1e24b38b9"},
	"checkout-branch":            {defaultURL, tempFolder(), "branch"},
	"clone":                      {defaultURL, tempFolder()},
	"commit":                     {cloneRepository(defaultURL, tempFolder())},
	"context":                    {defaultURL, tempFolder()},
	"custom_http":                {defaultURL},
	"find-if-any-tag-point-head": {cloneRepository(defaultURL, tempFolder())},
	"ls":                         {cloneRepository(defaultURL, tempFolder()), "HEAD", "vendor"},
	"ls-remote":                  {defaultURL},
	"merge_base":                 {cloneRepository(defaultURL, tempFolder()), "--is-ancestor", "HEAD~3", "HEAD^"},
	"open":                       {cloneRepository(defaultURL, tempFolder())},
	"progress":                   {defaultURL, tempFolder()},
	"pull":                       {createRepositoryWithRemote(tempFolder(), defaultURL)},
	"push":                       {setEmptyRemote(cloneRepository(defaultURL, tempFolder()))},
	"revision":                   {cloneRepository(defaultURL, tempFolder()), "master~2^"},
	"sha256":                     {tempFolder()},
	"showcase":                   {defaultURL, tempFolder()},
	"tag":                        {cloneRepository(defaultURL, tempFolder())},
}

// tests not working / set-up
var ignored = map[string]bool{
	"azure_devops":    true,
	"ls":              true,
	"sha256":          true,
	"submodule":       true,
	"tag-create-push": true,
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
	cmd := exec.Command("git", "clone", url, folder)
	err := cmd.Run()
	CheckIfError(err)

	return folder
}

func createBareRepository(dir string) string {
	return createRepository(dir, true)
}

func createRepository(dir string, isBare bool) string {
	var cmd *exec.Cmd
	if isBare {
		cmd = exec.Command("git", "init", "--bare", dir)
	} else {
		cmd = exec.Command("git", "init", dir)
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
	cmd := exec.Command("git", "remote", "set-url", "origin", remote)
	cmd.Dir = local
	err := cmd.Run()
	CheckIfError(err)
}

func addRemote(local, remote string) {
	cmd := exec.Command("git", "remote", "add", "origin", remote)
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
