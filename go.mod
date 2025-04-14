module github.com/go-git/go-git/v5

// go-git supports the last 3 stable Go versions.
go 1.23.0

toolchain go1.24.1

// Use the v6-exp branch across go-git dependencies.
replace (
	github.com/go-git/gcfg => github.com/go-git/gcfg v1.5.1-0.20240812080926-1b398f6213c9
	github.com/go-git/go-billy/v5 => github.com/go-git/go-billy/v5 v5.0.0-20240804231525-dc481f5289ba
	github.com/go-git/go-git-fixtures/v5 => github.com/go-git/go-git-fixtures/v5 v5.0.0-20241203230421-0753e18f8f03
)

require (
	dario.cat/mergo v1.0.1
	github.com/Microsoft/go-winio v0.6.2
	github.com/ProtonMail/go-crypto v1.2.0
	github.com/armon/go-socks5 v0.0.0-20160902184237-e75332964ef5
	github.com/elazarl/goproxy v1.7.2
	github.com/emirpasic/gods v1.18.1
	github.com/gliderlabs/ssh v0.3.8
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376
	github.com/go-git/go-billy/v5 v5.6.0
	github.com/go-git/go-git-fixtures/v4 v4.3.2-0.20231010084843-55a94097c399
	github.com/go-git/go-git-fixtures/v5 v5.0.0-20241203230421-0753e18f8f03
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8
	github.com/kevinburke/ssh_config v1.2.0
	github.com/pjbgf/sha1cd v0.3.2
	github.com/sergi/go-diff v1.3.2-0.20230802210424-5b0b94c5c0d3
	github.com/stretchr/testify v1.10.0
	golang.org/x/crypto v0.36.0
	golang.org/x/exp v0.0.0-20250218142911-aa4b98e5adaa
	golang.org/x/net v0.38.0
	golang.org/x/sys v0.31.0
	golang.org/x/text v0.23.0
)

require (
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/cloudflare/circl v1.6.0 // indirect
	github.com/cyphar/filepath-securejoin v0.4.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
