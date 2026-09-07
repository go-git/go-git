package gitenv

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// resolveAppleGit bypasses only Apple's tool launcher, not another Git selected
// through PATH. With an isolated HOME, the launcher can run xcodebuild -license
// check before starting Git. Under concurrent test load this exceeded the git
// transport tests' readiness deadline (go-git/go-git#2361); invoking the selected
// Git directly retained isolation without that startup delay.
//
// Resolve with the caller's environment, then apply isolation to Git itself.
// Do not cache: PATH and Xcode selection variables can change between tests.
func resolveAppleGit(cmd *exec.Cmd) {
	if runtime.GOOS != "darwin" || cmd.Err != nil || cmd.Path != "/usr/bin/git" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/xcrun", "--find", "git").Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		cmd.Err = fmt.Errorf("gitenv: resolve Apple Git with xcrun: %w", err)
		return
	}

	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) || path == "/usr/bin/git" {
		cmd.Err = fmt.Errorf("gitenv: resolve Apple Git: xcrun returned invalid executable %q", path)
		return
	}

	cmd.Path = path
}
