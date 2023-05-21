# go-git: examples

Here you can find a list of annotated _go-git_ examples:

### Basic
- [showcase](showcase/main.go) - A small showcase of the capabilities of _go-git_.
- [open](open/main.go) - Opening a existing repository cloned by _git_.
- [clone](clone/main.go) - Cloning a repository.
    - [username and password](clone/auth/basic/username_password/main.go) - Cloning a repository
      using a username and password.
    - [personal access token](clone/auth/basic/access_token/main.go) - Cloning
      a repository using a GitHub personal access token.
    - [ssh private key](clone/auth/ssh/main.go) - Cloning a repository using a ssh private key.
- [commit](commit/main.go) - Commit changes to the current branch to an existent repository.
- [push](push/main.go) - Push repository to default remote (origin).
- [pull](pull/main.go) - Pull changes from a remote repository.
- [checkout](checkout/main.go) - Check out a specific commit from a repository.
- [log](log/main.go) - Emulate `git log` command output iterating all the commit history from HEAD reference.
- [branch](branch/main.go) - How to create and remove branches or any other kind of reference.
- [tag](tag/main.go) - List/print repository tags.
- [tag create and push](tag-create-push/main.go) - Create and push a new tag.
- [tag find if head is tagged](find-if-any-tag-point-head/main.go) - Find if `HEAD` is tagged.
- [remotes](remotes/main.go) - Working with remotes: adding, removing, etc.
- [progress](progress/main.go) - Printing the progress information from the sideband.
- [revision](revision/main.go) - Solve a revision into a commit.
- [submodule](submodule/main.go) - Submodule update remote.

### Advanced
- [custom_http](custom_http/main.go) - Replacing the HTTP client using a custom one.
- [clone with context](context/main.go) - Cloning a repository with graceful cancellation.
- [storage](storage/README.md) - Implementing a custom storage system.
