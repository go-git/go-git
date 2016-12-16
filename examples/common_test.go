package examples

import (
	"flag"
	"go/build"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var examplesTest = flag.Bool("examples", false, "run the examples tests")

var defaultURL = "https://github.com/mcuadros/basic.git"

var args = map[string][]string{
	"showcase":    []string{defaultURL},
	"custom_http": []string{defaultURL},
	"clone":       []string{defaultURL, tempFolder()},
	"progress":    []string{defaultURL, tempFolder()},
	"open":        []string{filepath.Join(cloneRepository(defaultURL, tempFolder()), ".git")},
}

var ignored = map[string]bool{
	"storage": true,
}

var tempFolders = []string{}

func TestExamples(t *testing.T) {
	flag.Parse()
	if !*examplesTest && os.Getenv("CI") == "" {
		t.Skip("skipping examples tests, pass --examples to execute it")
		return
	}

	defer deleteTempFolders()

	examples, err := filepath.Glob(examplesFolder())
	if err != nil {
		t.Errorf("error finding tests: %s", err)
	}

	for _, example := range examples {
		_, name := filepath.Split(filepath.Dir(example))

		if ignored[name] {
			continue
		}

		t.Run(name, func(t *testing.T) {
			testExample(t, name, example)
		})
	}
}

func tempFolder() string {
	path, err := ioutil.TempDir("", "")
	CheckIfError(err)

	tempFolders = append(tempFolders, path)
	return path
}

func packageFolder() string {
	return filepath.Join(
		build.Default.GOPATH,
		"src", "gopkg.in/src-d/go-git.v4",
	)
}

func examplesFolder() string {
	return filepath.Join(
		packageFolder(),
		"examples", "*", "main.go",
	)
}

func cloneRepository(url, folder string) string {
	cmd := exec.Command("git", "clone", url, folder)
	err := cmd.Run()
	CheckIfError(err)

	return folder
}

func testExample(t *testing.T, name, example string) {
	cmd := exec.Command("go", append([]string{
		"run", filepath.Join(example),
	}, args[name]...)...)

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
