package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	bin = "go-git"

	receivePackName = "receive-pack"
	uploadPackName  = "upload-pack"
	versionName     = "version"

	receivePackBin      = "git-receive-pack"
	uploadPackBin       = "git-upload-pack"
	updateServerInfoBin = "update-server-info"
	usage               = `Please specify one command of: receive-pack, upload-pack or version
Usage:
	go-git [OPTIONS] <receive-pack | upload-pack | version>

Help Options:
	-h, --help  Show this help message

Available commands:
	receive-pack
	upload-pack
	version       Show the version information.
`

	receiveUploadUsageFormat = "usage: %s <git-dir>\n"

	// Exit codes are defined in api-error-handling upstream:
	// https://github.com/git/git/blob/8be77c5de65442b331a28d63802c7a3b94a06c5a/Documentation/technical/api-error-handling.txt#L32-L46
	cannotStartExitCode      = 129
	fatalApplicationExitCode = 128
	generalErrorExitCode     = -1
)

var (
	// keeps a copy of the original command name, in case it was changed.
	originalCommand = os.Args[0]

	commands = map[string]func([]string) error{
		updateServerInfoBin: updateServerInfoRun,
		receivePackName:     receivePackRun,
		uploadPackName:      uploadPackRun,
		versionName:         versionRun,
	}
)

func main() {
	switch filepath.Base(os.Args[0]) {
	case receivePackBin:
		os.Args = append([]string{"git", receivePackName}, os.Args[1:]...)
	case uploadPackBin:
		os.Args = append([]string{"git", uploadPackName}, os.Args[1:]...)
	}

	if len(os.Args) < 2 {
		showUsage()
		os.Exit(cannotStartExitCode)
	}

	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	cmd, ok := commands[os.Args[1]]
	if !ok {
		showUsage()
		os.Exit(cannotStartExitCode)
	}

	if err := cmd(args); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(generalErrorExitCode)
	}
}

func showUsage() {
	fmt.Print(usage)
}

func showReceiveUploadUsage() {
	fmt.Printf(receiveUploadUsageFormat, originalCommand)
}
