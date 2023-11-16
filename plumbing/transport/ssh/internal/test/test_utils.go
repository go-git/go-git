package test

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	. "gopkg.in/check.v1"
)

func HandlerSSH(c *C) func(s ssh.Session) {
	return func(s ssh.Session) {
		cmd, stdin, stderr, stdout, err := buildCommand(s.Command())
		if err != nil {
			fmt.Println(err)
			return
		}

		if err := cmd.Start(); err != nil {
			fmt.Println(err)
			return
		}

		go func() {
			defer stdin.Close()
			io.Copy(stdin, s)
		}()

		var wg sync.WaitGroup
		wg.Add(2)

		// Tee stderr
		var stderrBuf bytes.Buffer
		defer func() {
			if stderrBuf.Len() > 0 {
				c.Logf("stderr: %s", stderrBuf.String())
			}
		}()

		go func() {
			defer wg.Done()
			tee := io.TeeReader(stderr, &stderrBuf)
			io.Copy(s.Stderr(), tee)
		}()

		go func() {
			defer wg.Done()
			io.Copy(s, stdout)
		}()

		wg.Wait()

		if err := cmd.Wait(); err != nil {
			c.Logf("%s: command failed: %s", c.TestName(), err)
			return
		}
	}
}

func buildCommand(c []string) (cmd *exec.Cmd, stdin io.WriteCloser, stderr, stdout io.ReadCloser, err error) {
	if len(c) != 2 {
		err = fmt.Errorf("invalid command")
		return
	}

	// fix for Windows environments
	var path string
	if runtime.GOOS == "windows" {
		path = strings.Replace(c[1], "/C:/", "C:/", 1)
	} else {
		path = c[1]
	}

	cmd = exec.Command(c[0], path)
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	stdin, err = cmd.StdinPipe()
	if err != nil {
		return
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	return
}
