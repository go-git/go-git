package main

import (
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/utils/trace"
	"github.com/jessevdk/go-flags"
)

const (
	bin            = "go-git"
	receivePackBin = "git-receive-pack"
	uploadPackBin  = "git-upload-pack"
)

func init() {
	if t := os.Getenv("GIT_TRACE"); t == "1" {
		trace.SetTarget(trace.General | trace.Packet)
	}
}

func main() {
	switch filepath.Base(os.Args[0]) {
	case receivePackBin:
		os.Args = append([]string{"git", "receive-pack"}, os.Args[1:]...)
	case uploadPackBin:
		os.Args = append([]string{"git", "upload-pack"}, os.Args[1:]...)
	}

	parser := flags.NewNamedParser(bin, flags.Default)
	parser.AddCommand("update-server-info", "", "", &CmdUpdateServerInfo{})
	parser.AddCommand("receive-pack", "", "", &CmdReceivePack{})
	parser.AddCommand("upload-pack", "", "", &CmdUploadPack{})
	parser.AddCommand("version", "Show the version information.", "", &CmdVersion{})
	parser.AddCommand("clone", "", "", &CmdClone{})

	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrCommandRequired {
			parser.WriteHelp(os.Stdout)
		}

		os.Exit(1)
	}
}

type cmd struct {
	Verbose bool `short:"v" description:"Activates the verbose mode"`
}
