//go:build unix

package git

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func startGitDaemon(base string, port int) (*exec.Cmd, error) {
	daemon := exec.Command(
		"git",
		"daemon",
		fmt.Sprintf("--base-path=%s", base),
		"--export-all",
		"--enable=receive-pack",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", port),
		// Unless max-connections is limited to 1, a git-receive-pack
		// might not be seen by a subsequent operation.
		"--max-connections=1",
	)

	// new PGID should be set in order to kill the child process spawned by the command.
	daemon.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Environment must be inherited in order to acknowledge GIT_EXEC_PATH if set.
	daemon.Env = os.Environ()
	if err := daemon.Start(); err != nil {
		return nil, err
	}

	// Wait until daemon is ready.
	if err := waitForPort(port); err != nil {
		return nil, err
	}

	return daemon, nil
}

func killDaemon(daemon *exec.Cmd) error {
	return syscall.Kill(-daemon.Process.Pid, syscall.SIGINT)
}
