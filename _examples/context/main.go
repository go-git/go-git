package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
)

// Graceful cancellation example of a basic git operation such as Clone.
func main() {
	CheckArgs("<url>", "<directory>")
	url := os.Args[1]
	directory := os.Args[2]

	// Clone the given repository to the given directory
	Info("git clone %s %s", url, directory)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	// The context is the mechanism used by go-git, to support deadlines and
	// cancellation signals.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cancel when we are finished consuming integers

	go func() {
		<-stop
		Warning("\nSignal detected, canceling operation...")
		cancel()
	}()

	Warning("To gracefully stop the clone operation, push Crtl-C.")

	// Using PlainCloneContext we can provide to a context, if the context
	// is cancelled, the clone operation stops gracefully.
	_, err := git.PlainCloneContext(ctx, directory, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
	})

	// If the context was cancelled, an error is returned.
	CheckIfError(err)
}
