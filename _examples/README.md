# go-git: examples

Here you can find a list of annotated _go-git_ examples:

### Basic
- [showcase](showcase/main.go) - A small showcase of the capabilities of _go-git_
- [open](open/main.go) - Opening a existing repository cloned by _git_
- [clone](clone/main.go) - Cloning a repository
- [clone with context](context/main.go) - Cloning a repository with graceful cancellation.
- [log](log/main.go) - Emulate `git log` command output iterating all the commit history from HEAD reference
- [remotes](remotes/main.go) - Working with remotes: adding, removing, etc
- [progress](progress/main.go) - Printing the progress information from the sideband
- [push](push/main.go) - Push repository to default remote (origin)
- [checkout](checkout/main.go) - Check out a specific commit from a repository
- [tag](tag/main.go) - List/print repository tags
- [pull](pull/main.go) - Pull changes from a remote repository
- [revision](revision/main.go) - Solve a revision into a commit
### Advanced
- [custom_http](custom_http/main.go) - Replacing the HTTP client using a custom one
- [storage](storage/README.md) - Implementing a custom storage system
