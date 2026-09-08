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

// resolveTimeout bounds the xcrun call below, for the caller whose context
// carries no deadline: a launcher that is running a license check is slow
// rather than stuck, and a resolution that never returns would hang the test
// binary in place of the startup delay this avoids.
const resolveTimeout = 30 * time.Second

// resolveAppleGit bypasses only Apple's tool launcher, not another Git selected
// through PATH. With an isolated HOME, the launcher can run xcodebuild -license
// check before starting Git. Under concurrent test load this exceeded the git
// transport tests' readiness deadline (go-git/go-git#2361); invoking the selected
// Git directly retained isolation without that startup delay.
//
// Resolve with the caller's environment, then apply isolation to Git itself.
// Do not cache: PATH and Xcode selection variables can change between tests.
//
// xcrun runs under ctx, so a caller that supplies one is not left waiting on a
// subprocess outside it, with resolveTimeout capping a context that carries no
// deadline of its own. Whichever ends first, the context reports why: exec
// kills xcrun and would otherwise report only the signal.
func resolveAppleGit(ctx context.Context, cmd *exec.Cmd) {
	if runtime.GOOS != "darwin" || cmd.Err != nil || cmd.Path != "/usr/bin/git" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/xcrun", "--find", "git").Output()
	if err != nil {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
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
