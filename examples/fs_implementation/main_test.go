package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/alcortesm/tgz"
)

func TestMain(m *testing.M) {
	setUp()
	rval := m.Run()
	tearDown()
	os.Exit(rval)
}

func setUp() {
	var err error
	repo, err = tgz.Extract("../../storage/seekable/internal/gitdir/fixtures/spinnaker-gc.tgz")
	if err != nil {
		panic(err)
	}
}

var repo string

func tearDown() {
	err := os.RemoveAll(repo)
	if err != nil {
		panic(err)
	}
}

func TestJoin(t *testing.T) {
	fs := newFS("")
	for i, test := range [...]struct {
		input    []string
		expected string
	}{
		{
			input:    []string{},
			expected: "",
		}, {
			input:    []string{"a"},
			expected: "a",
		}, {
			input:    []string{"a", "b"},
			expected: "a--b",
		}, {
			input:    []string{"a", "b", "c"},
			expected: "a--b--c",
		},
	} {
		obtained := fs.Join(test.input...)
		if obtained != test.expected {
			t.Fatalf("test %d:\n\tinput = %v\n\tobtained = %v\n\texpected = %v\n",
				i, test.input, obtained, test.expected)
		}
	}
}

func TestStat(t *testing.T) {
	fs := newFS(filepath.Join(repo, ".git/"))
	for i, path := range [...]string{
		"index",
		"info--refs",
		"objects--pack--pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack",
	} {
		real, err := os.Open(fs.ToReal(path))
		if err != nil {
			t.Fatalf("test %d: openning real: %s", err)
		}

		expected, err := real.Stat()
		if err != nil {
			t.Fatalf("test %d: stat on real: %s", err)
		}

		obtained, err := fs.Stat(path)
		if err != nil {
			t.Fatalf("test %d: fs.Stat unexpected error: %s", i, err)
		}

		if !reflect.DeepEqual(obtained, expected) {
			t.Fatalf("test %d:\n\tinput = %s\n\tobtained = %v\n\texpected = %v\n",
				i, path, obtained, expected)
		}

		err = real.Close()
		if err != nil {
			t.Fatalf("test %d: closing real: %s", i, err)
		}
	}
}

func TestStatErrors(t *testing.T) {
	fs := newFS(filepath.Join(repo, ".git/"))
	for i, test := range [...]struct {
		input     string
		errRegExp string
	}{
		{
			input:     "bla",
			errRegExp: ".*bla: no such file or directory",
		}, {
			input:     "bla--foo",
			errRegExp: ".*bla/foo: no such file or directory",
		},
	} {
		expected := regexp.MustCompile(test.errRegExp)

		_, err := fs.Stat(test.input)
		if err == nil {
			t.Fatalf("test %d: no error returned", i)
		}
		if !expected.MatchString(err.Error()) {
			t.Fatalf("test %d: error missmatch\n\tobtained = %q\n\texpected regexp = %q\n",
				i, err.Error(), test.errRegExp)
		}
	}
}

func TestOpen(t *testing.T) {
	fs := newFS(filepath.Join(repo, ".git/"))
	for i, path := range [...]string{
		"index",
		"info--refs",
		"objects--pack--pack-584416f86235cac0d54bfabbdc399fb2b09a5269.pack",
	} {
		real, err := os.Open(fs.ToReal(path))
		if err != nil {
			t.Fatalf("test %d: openning real: %s", err)
		}

		realData, err := ioutil.ReadAll(real)
		if err != nil {
			t.Fatal("test %d: ioutil.ReadAll on real: %s", err)
		}

		err = real.Close()
		if err != nil {
			t.Fatal("test %d: closing real: %s", err)
		}

		obtained, err := fs.Open(path)
		if err != nil {
			t.Fatalf("test %d: fs.Open unexpected error: %s", i, err)
		}

		obtainedData, err := ioutil.ReadAll(obtained)
		if err != nil {
			t.Fatal("test %d: ioutil.ReadAll on obtained: %s", err)
		}

		err = obtained.Close()
		if err != nil {
			t.Fatal("test %d: closing obtained: %s", err)
		}

		if !reflect.DeepEqual(obtainedData, realData) {
			t.Fatalf("test %d:\n\tinput = %s\n\tobtained = %v\n\texpected = %v\n",
				i, path, obtainedData, realData)
		}
	}
}

func TestReadDir(t *testing.T) {
	fs := newFS(filepath.Join(repo, ".git/"))
	for i, path := range [...]string{
		"info",
		".",
		"",
		"objects",
		"objects--pack",
	} {
		expected, err := ioutil.ReadDir(fs.ToReal(path))
		if err != nil {
			t.Fatalf("test %d: real ReadDir: %s", err)
		}

		obtained, err := fs.ReadDir(path)
		if err != nil {
			t.Fatalf("test %d: fs.ReadDir unexpected error: %s", i, err)
		}

		if !reflect.DeepEqual(obtained, expected) {
			t.Fatalf("test %d:\n\tinput = %s\n\tobtained = %v\n\texpected = %v\n",
				i, path, obtained, expected)
		}
	}
}
