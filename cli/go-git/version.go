package main

import "fmt"

var version string
var build string

func versionRun(args []string) error {
	fmt.Printf("%s (%s) - build %s\n", bin, version, build)

	return nil
}
