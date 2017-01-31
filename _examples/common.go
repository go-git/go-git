package examples

import (
	"os"
	"strings"

	"github.com/fatih/color"
)

func CheckArgs(arg ...string) {
	if len(os.Args) < len(arg)+1 {
		Warning("Usage: %s %s", os.Args[0], strings.Join(arg, " "))
		os.Exit(1)
	}
}

func CheckIfError(err error) {
	if err == nil {
		return
	}

	color.Red("error: %s", err)
	os.Exit(1)
}

func Info(format string, args ...interface{}) {
	color.Blue(format, args...)
}

func Warning(format string, args ...interface{}) {
	color.Cyan(format, args...)
}
