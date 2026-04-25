# Extending go-git

`go-git` was built in a highly extensible manner, which enables some of its functionalities to be changed or extended without the need of changing its codebase. Here are the key extensibility features:

## Dot Git Storers

Dot git storers are the components responsible for storing the Git internal files, including objects and references.

The built-in storer implementations include [memory](storage/memory) and [filesystem](storage/filesystem). The `memory` storer stores all the data in memory, and its use look like this:

```go
	r, err := git.Init(memory.NewStorage(), nil)
```

The `filesystem` storer stores the data in the OS filesystem, and can be used as follows:

```go
    r, err := git.Init(filesystem.NewStorage(osfs.New("/tmp/foo")), nil)
```

New implementations can be created by implementing the [storage.Storer interface](storage/storer.go#L16).

## Filesystem

Git repository worktrees are managed using a filesystem abstraction based on [go-billy](https://github.com/go-git/go-billy). The Git operations will take place against the specific filesystem implementation. Initialising a repository in Memory can be done as follows:

```go
	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), fs)
```

The same operation can be done against the OS filesystem:

```go
    fs := osfs.New("/tmp/foo")
    r, err := git.Init(memory.NewStorage(), fs)
```

New filesystems (e.g. cloud based storage) could be created by implementing `go-billy`'s [Filesystem interface](https://github.com/go-git/go-billy/blob/326c59f064021b821a55371d57794fbfb86d4cb3/fs.go#L52).

## Transport Schemes

Git supports various transport schemes, including `http`, `https`, `ssh`, `git`, `file`. `go-git` defines the [transport.Transport interface](plumbing/transport/common.go#L48) to represent them.

The built-in implementations can be replaced by calling `transport.Register`.

An example of changing the built-in `https` implementation to skip TLS could look like this:

```go
	customClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	transport.Register("https", githttp.NewTransport(&githttp.TransportOptions{Client: customClient}))
```

Some internal implementations enables code reuse amongst the different transport implementations. Some of these may be made public in the future (e.g. `plumbing/transport/internal/common`).

## Cache

Several different operations across `go-git` lean on caching of objects in order to achieve optimal performance. The caching functionality is defined by the [cache.Object interface](plumbing/cache/common.go#L17).

Two built-in implementations are `cache.ObjectLRU` and `cache.BufferLRU`. However, the caching functionality can be customized by implementing the interface `cache.Object` interface.

## Hash

`go-git` uses the `crypto.Hash` interface to represent hash functions. The built-in implementations are `github.com/pjbgf/sha1cd` for SHA1 and Go's `crypto/SHA256`.

The default hash functions can be changed by calling `hash.RegisterHash`.
```go
    func init() {
        hash.RegisterHash(crypto.SHA1, sha1.New)
    }
```

New `SHA1` or `SHA256` hash functions that implement the `hash.RegisterHash` interface can be registered by calling `RegisterHash`.

## Compression

`go-git` uses zlib compression for loose objects and packfile entries. By default it uses the Go standard library's `compress/zlib`, registered at init time as the [`plugin.Zlib()`](x/plugin/plugin_zlib.go) provider. Register an alternative `plugin.ZlibProvider` — for example [`github.com/klauspost/compress/zlib`](https://github.com/klauspost/compress) — to swap the implementation without `go-git` taking a direct dependency on it.

Register a provider during program init, before any `go-git` operation runs. Registration uses the [plugin system](#plugin-system-experimental) so it follows the same freeze-on-first-use lifecycle as other plugins:

```go
import (
    "fmt"
    "io"

    kpzlib "github.com/klauspost/compress/zlib"

    "github.com/go-git/go-git/v6/x/plugin"
)

type klauspostProvider struct{}

func (klauspostProvider) NewReader(r io.Reader) (plugin.ZlibReader, error) {
    zr, err := kpzlib.NewReader(r)
    if err != nil {
        return nil, err
    }
    zlr, ok := zr.(plugin.ZlibReader)
    if !ok {
        return nil, fmt.Errorf("klauspost reader %T does not implement plugin.ZlibReader", zr)
    }
    return zlr, nil
}

func (klauspostProvider) NewWriter(w io.Writer) plugin.ZlibWriter {
    return kpzlib.NewWriter(w)
}

func init() {
    err := plugin.Register(plugin.Zlib(), func() plugin.ZlibProvider {
        return klauspostProvider{}
    })
    if err != nil {
        panic(err)
    }
}
```

Registering after `go-git` has already resolved the zlib provider (on the first pool miss or `sync.NewZlibWriter` call) returns `plugin.ErrFrozen` and the existing provider stays in effect.

## Plugin System (Experimental)

> **Note:** The plugin system is experimental and its API may change in future releases.

`go-git` provides a plugin registry in the [`x/plugin`](x/plugin) package that enables off-tree implementations of specific features to be registered and used at runtime, without modifying the core codebase.

Each plugin is identified by a typed key. Registrations must happen before the first call to `Get` for a given key (typically in a `func init()`), after which the entry is frozen and no further registrations are accepted.

### Object Signing

The first feature exposed via the plugin system is object signing. When an `ObjectSigner` plugin is registered, it becomes the default signer for new commits and tags.

```go
import (
    "github.com/go-git/go-git/v6/x/plugin"
)

func init() {
    plugin.Register(plugin.ObjectSigner(), func() plugin.Signer {
        return &mySigner{}
    })
}
```

Where `mySigner` implements the `plugin.Signer` interface:

```go
type Signer interface {
    Sign(message io.Reader) ([]byte, error)
}
```

### Config Loader

The `ConfigLoader` plugin controls how global and system-level Git configuration are loaded. By default, the Auto plugin is registered, mimicking Git behaviour.
To override this, register a `ConfigSource` implementation based on your needs: static configs, custom backends, etc.
To completely ignore System and Global configs:

```go
import (
    "github.com/go-git/go-git/v6/x/plugin"
    xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

func init() {
    plugin.Register(plugin.ConfigLoader(), func() plugin.ConfigSource {
        return xconfig.NewEmpty()
    })
}
```

The `ConfigSource` interface has a single method:

```go
type ConfigSource interface {
    Load(scope config.Scope) (config.ConfigStorer, error)
}
```

Built-in implementations in [`x/plugin/config`](x/plugin/config):

- **`NewAuto()`** mimics default Git behaviour, where environment variables override the filesystem defaults.
- **`NewStatic(global, system)`** returns fixed configs provided at construction time, useful for testing and embedded use.
- **`NewEmpty()`** returns empty configs for both scopes.

For more information, refer to the [`x/plugin` package documentation](x/plugin/plugin.go).
