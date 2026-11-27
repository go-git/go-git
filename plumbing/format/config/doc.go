// Package config implements encoding and decoding of Git configuration
// files.
//
// A configuration file is a list of sections, each holding options and,
// optionally, named subsections with options of their own:
//
//	[core]
//		bare = false
//	[remote "origin"]
//		url = https://example.com/repo.git
//		fetch = +refs/heads/*:refs/remotes/origin/*
//
// A [Decoder] reads a file into a [Config], and an [Encoder] writes one
// back. The package deals with the syntax only and gives no meaning to the
// options, which is left to the go-git config package.
//
// The syntax is specified in git-config(1), under "CONFIGURATION FILE":
// https://git-scm.com/docs/git-config#_configuration_file
package config
