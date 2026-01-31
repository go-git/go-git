package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	. "github.com/go-git/go-git/v6/_examples"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Example of how to verify SSH signatures on commits.
// This demonstrates loading an allowed_signers file and verifying
// the signature on the HEAD commit.
//
// Usage:
//
//	verify-ssh-signature <repository-path> [allowed-signers-file]
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

	// Get the HEAD commit
	ref, err := r.Head()
	CheckIfError(err)

	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	Info("Verifying signature on commit %s", commit.Hash)

	// Verify the signature
	result, err := commit.VerifySignature(verifier)
	CheckIfError(err)

	// Display the verification result
	fmt.Println("\nSignature Verification Result:")
	fmt.Println("==============================")
	fmt.Printf("Valid:       %v\n", result.Valid)
	fmt.Printf("Type:        %s\n", result.Type)
	fmt.Printf("Trust Level: %s\n", result.TrustLevel)
	fmt.Printf("Key ID:      %s\n", result.KeyID)
	fmt.Printf("Signer:      %s\n", result.Signer)

	if result.Error != nil {
		fmt.Printf("Error:       %v\n", result.Error)
	}

	fmt.Println()

	// Display trust status
	if result.IsValid() {
		if result.IsTrusted(object.TrustFull) {
			fmt.Println("Status: Signature is VALID and TRUSTED")
		} else {
			fmt.Println("Status: Signature is VALID but NOT TRUSTED (key not in allowed_signers)")
		}
	} else {
		fmt.Println("Status: Signature is INVALID")
	}

	// Display commit details
	fmt.Println("\nCommit Details:")
	fmt.Println("===============")
	fmt.Printf("Hash:    %s\n", commit.Hash)
	fmt.Printf("Author:  %s <%s>\n", commit.Author.Name, commit.Author.Email)
	fmt.Printf("Date:    %s\n", commit.Author.When.Format("Mon Jan 2 15:04:05 2006 -0700"))
	fmt.Printf("Message: %s\n", commit.Message)
}
