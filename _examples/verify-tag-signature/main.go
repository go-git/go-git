package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Example of how to verify SSH signatures on tags.
// This demonstrates loading an allowed_signers file and verifying
// signatures on annotated tags in a repository.
//
// Usage:
//
//	verify-tag-signature <repository-path> [allowed-signers-file]
//
// If allowed-signers-file is not provided, it will be loaded from
// the git config (gpg.ssh.allowedSignersFile).
func main() {
	CheckArgs("<repository-path>")
	repoPath := os.Args[1]

	// Open the repository
	Info("Opening repository at %s", repoPath)
	r, err := git.PlainOpen(repoPath)
	CheckIfError(err)

	// Load the SSH verifier
	var verifier *git.SSHVerifier

	if len(os.Args) > 2 {
		// Use provided allowed_signers file
		allowedSignersPath := os.Args[2]
		Info("Loading allowed signers from %s", allowedSignersPath)
		verifier, err = git.NewSSHVerifierFromFile(allowedSignersPath)
		CheckIfError(err)
	} else {
		// Load from git config (local + global)
		Info("Loading allowed signers from git config")
		cfg, err := r.ConfigScoped(config.GlobalScope)
		CheckIfError(err)

		verifier, err = git.NewSSHVerifierFromConfig(cfg)
		CheckIfError(err)

		if verifier == nil {
			fmt.Println("No allowed signers file configured in git config.")
			fmt.Println("Set gpg.ssh.allowedSignersFile or provide path as argument.")
			os.Exit(1)
		}

		Info("Using allowed signers from %s", cfg.GPG.SSH.AllowedSignersFile)
	}

	// Get all annotated tags
	Info("Fetching annotated tags")
	tags, err := r.TagObjects()
	CheckIfError(err)

	// Collect tags into a slice so we can show the last few
	var tagList []*object.Tag
	err = tags.ForEach(func(t *object.Tag) error {
		tagList = append(tagList, t)
		return nil
	})
	CheckIfError(err)

	if len(tagList) == 0 {
		fmt.Println("No annotated tags found in repository.")
		os.Exit(0)
	}

	// Sort tags by date, most recent first
	sort.Slice(tagList, func(i, j int) bool {
		return tagList[i].Tagger.When.After(tagList[j].Tagger.When)
	})

	// Show up to 5 most recent tags
	count := len(tagList)
	if count > 5 {
		count = 5
	}

	fmt.Printf("\nVerifying %d most recent annotated tags:\n", count)
	fmt.Println("==========================================")

	for _, tag := range tagList[:count] {
		fmt.Printf("\nTag: %s\n", tag.Name)
		fmt.Printf("Target: %s\n", tag.Target)
		fmt.Printf("Tagger: %s <%s>\n", tag.Tagger.Name, tag.Tagger.Email)
		fmt.Printf("Date: %s\n", tag.Tagger.When.Format("Mon Jan 2 15:04:05 2006 -0700"))

		if tag.PGPSignature == "" {
			fmt.Println("Signature: NOT SIGNED")
			continue
		}

		// Verify the signature
		result, err := tag.VerifySignature(verifier)
		if err != nil {
			fmt.Printf("Signature: ERROR - %v\n", err)
			continue
		}

		fmt.Printf("Signature Type: %s\n", result.Type)
		fmt.Printf("Valid: %v\n", result.Valid)
		fmt.Printf("Trust Level: %s\n", result.TrustLevel)
		fmt.Printf("Key ID: %s\n", result.KeyID)

		if result.Signer != "" {
			fmt.Printf("Signer: %s\n", result.Signer)
		}

		if result.Error != nil {
			fmt.Printf("Verification Error: %v\n", result.Error)
		}

		// Display trust status
		if result.IsValid() {
			if result.IsTrusted(object.TrustFull) {
				fmt.Println("Status: VALID and TRUSTED")
			} else {
				fmt.Println("Status: VALID but NOT TRUSTED")
			}
		} else {
			fmt.Println("Status: INVALID")
		}
	}

	fmt.Println()
}
