package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
)

func fatal(format string, a ...interface{}) {
	log.Printf("FATAL: "+format, a...)
	os.Exit(1)
}

func gitStatus() {
	cmd := exec.Command("git", "status", "-s")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return
	}

	if len(out) < 1 {
		//log.Printf("Nothing changed")
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) > 0 {
			log.Printf(line)
		}
	}
}

func main() {
	repositoryPath, err := os.Getwd()
	if err != nil {
		fatal("Cannot parse runtime path: %s\n", err)
	}

	repositoryPath = strings.ReplaceAll(repositoryPath, "\\", "/")
	r, err := git.PlainOpen(repositoryPath)
	if err != nil {
		fatal("Cannot open repository: %s\n", err)
	}

	w, err := r.Worktree()
	if err != nil {
		fatal("Cannot access repository: %s\n", err)
	}

	log.Printf("=================================================")
	log.Printf("Files in directory:")
	files, err := ioutil.ReadDir(repositoryPath)
	if err != nil {
		fatal("Cannot read files in directory: %s\n", err)
	}
	for _, f := range files {
		log.Printf(f.Name())
	}

	log.Printf("=================================================")
	log.Printf(".gitignore content:")
	dat, err := ioutil.ReadFile(repositoryPath + "/.gitignore")
	if err != nil {
		fatal("Cannot read .gitignore: %s\n", err)
	}

	for _, line := range strings.Split(string(dat), "\n") {
		if len(line) > 0 {
			log.Printf(line)
		}
	}

	status, err := w.Status()
	if err != nil {
		fatal("Cannot retrieve git status: %s\n", err)
	}

	log.Printf("=================================================")
	log.Printf("go-git status:")
	for path, s := range status {
		log.Printf("%s %s", string(s.Worktree), path)
	}
	log.Printf("=================================================")
	log.Printf("git status:")
	gitStatus()
	log.Printf("=================================================")
}
