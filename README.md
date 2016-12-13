# go-git [![GoDoc](https://godoc.org/gopkg.in/src-d/go-git.v4?status.svg)](https://godoc.org/gopkg.in/src-d/go-git.v4) [![Build Status](https://travis-ci.org/src-d/go-git.svg)](https://travis-ci.org/src-d/go-git) [![codecov.io](https://codecov.io/github/src-d/go-git/coverage.svg)](https://codecov.io/github/src-d/go-git) [![codebeat badge](https://codebeat.co/badges/b6cb2f73-9e54-483d-89f9-4b95a911f40c)](https://codebeat.co/projects/github-com-src-d-go-git)

A low level and highly extensible git implementation in **pure Go**. 

*go-git* aims to reach the completeness of [libgit2](https://libgit2.github.com/) or [jgit](http://www.eclipse.org/jgit/), nowadays covers the **majority** of the plumbing **read operations** and **some** of the main **write operations**, but lacks the main porcelain operations such as merges.

It is **highly extensible**, we have been following the open/close principle in its design to facilitate extensions, mainly focusing the efforts on the persistence of the objects.

### ... is this production ready?

The master branch represents the `v4` of the library, it is currently under active development and is planned to be released in early 2017.

If you are looking for a production ready version, please take a look to the [`v3`](https://github.com/src-d/go-git/tree/v3) which is being used in production at [source{d}](http://sourced.tech) since August 2015 to analyze all GitHub public repositories (i.e. 16M repositories).

We recommend the use of `v4` to develop new projects since it includes much new functionality and provides a more *idiomatic git* API 

Installation
------------

The recommended way to install *go-git* is:

```
go get -u gopkg.in/src-d/go-git.v4/...
```


Examples
--------

Cloning a repository and printing the history of HEAD, just like `git log` does

> Please note that the functions `CheckIfError` and `Inf`o used in the examples are from the [examples package](https://github.com/src-d/go-git/blob/master/examples/common.go#L17) just to be used in the examples.


```go
// Instances an in-memory git repository
r := git.NewMemoryRepository()

// Clones the given repository, creating the remote, the local branches
// and fetching the objects, exactly as:
Info("git clone https://github.com/src-d/go-siva")

err := r.Clone(&git.CloneOptions{URL: "https://github.com/src-d/go-siva"})
CheckIfError(err)

// Gets the HEAD history from HEAD, just like does:
Info("git log")

// ... retrieves the branch pointed by HEAD
ref, err := r.Head()
CheckIfError(err)

// ... retrieves the commit object
commit, err := r.Commit(ref.Hash())
CheckIfError(err)

// ... retrieves the commit history
history, err := commit.History()
CheckIfError(err)

// ... just iterates over the commits, printing it
for _, c := range history {
    fmt.Println(c)
}
```

Outputs:
```
commit 2275fa7d0c75d20103f90b0e1616937d5a9fc5e6
Author: Máximo Cuadros <mcuadros@gmail.com>
Date:   2015-10-23 00:44:33 +0200 +0200

commit 35b585759cbf29f8ec428ef89da20705d59f99ec
Author: Carlos Cobo <toqueteos@gmail.com>
Date:   2015-05-20 15:21:37 +0200 +0200

commit 7e3259c191a9de23d88b6077dcb1cd427e925432
Author: Alberto Cortés <alberto@sourced.tech>
Date:   2016-01-21 03:29:57 +0100 +0100

commit 24b8ae50db91f3909b11304014564bffc6fdee79
Author: Alberto Cortés <alberto@sourced.tech>
Date:   2015-12-11 17:57:10 +0100 +0100
...
```

You can find this [example](examples/log/main.go) and many other at the [examples](examples) folder

Contribute
----------

If you are interested on contributing to go-git, open an [issue](https://github.com/src-d/go-git/issues) explaining which missing functionality you want to work in, and we will guide you through the implementation.

License
-------

MIT, see [LICENSE](LICENSE)
