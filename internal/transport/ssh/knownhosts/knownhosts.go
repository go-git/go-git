// Copyright 2024 Skeema LLC and the Skeema Knownhosts authors

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Originally from: https://github.com/skeema/knownhosts/blob/v1.3.0/knownhosts.go
// Upstream commit: 9485bdec8ed521b32ab0e9310619effa071a99d6

// Package knownhosts is a thin wrapper around golang.org/x/crypto/ssh/knownhosts,
// adding the ability to obtain the list of host key algorithms for a known host.
//
// It is a trimmed copy of github.com/skeema/knownhosts that keeps only what
// the default host key verification of go-git's SSH transport needs. The full
// API is available in the github.com/go-git/x/ssh/knownhosts module.
package knownhosts

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"

	"golang.org/x/crypto/ssh"
	xknownhosts "golang.org/x/crypto/ssh/knownhosts"

	"github.com/go-git/go-git/v6/utils/trace"
)

// HostKeyDB wraps logic in golang.org/x/crypto/ssh/knownhosts with additional
// behaviors, such as the ability to perform host key/algorithm lookups from
// known_hosts entries.
type HostKeyDB struct {
	callback   ssh.HostKeyCallback
	isCert     map[string]bool // keyed by "filename:line"
	isWildcard map[string]bool // keyed by "filename:line"
}

// NewDB creates a HostKeyDB from the given OpenSSH known_hosts file(s). It
// reads and parses the provided files one additional time (beyond logic in
// golang.org/x/crypto/ssh/knownhosts) in order to:
//
//   - Handle CA lines properly and return ssh.CertAlgo* values when calling the
//     HostKeyAlgorithms method, for use in ssh.ClientConfig.HostKeyAlgorithms
//   - Allow * wildcards in hostnames to match on non-standard ports, providing
//     a workaround for https://github.com/golang/go/issues/52056 in order to
//     align with OpenSSH's wildcard behavior
//
// When supplying multiple files, their order does not matter.
func NewDB(files ...string) (*HostKeyDB, error) {
	cb, err := xknownhosts.New(files...)
	if err != nil {
		return nil, err
	}
	hkdb := &HostKeyDB{
		callback:   cb,
		isCert:     make(map[string]bool),
		isWildcard: make(map[string]bool),
	}

	// Re-read each file a single time, looking for @cert-authority lines. The
	// logic for reading the file is designed to mimic hostKeyDB.Read from
	// golang.org/x/crypto/ssh/knownhosts
	for _, filename := range files {
		f, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Bytes()
			line = bytes.TrimSpace(line)
			// Does the line start with "@cert-authority" followed by whitespace?
			if len(line) > 15 && bytes.HasPrefix(line, []byte("@cert-authority")) && (line[15] == ' ' || line[15] == '\t') {
				mapKey := fmt.Sprintf("%s:%d", filename, lineNum)
				hkdb.isCert[mapKey] = true
				line = bytes.TrimSpace(line[16:])
			}
			// truncate line to just the host pattern field
			if i := bytes.IndexAny(line, "\t "); i >= 0 {
				line = line[:i]
			}
			// Does the host pattern contain a * wildcard and no specific port?
			if i := bytes.IndexRune(line, '*'); i >= 0 && !bytes.Contains(line[i:], []byte("]:")) {
				mapKey := fmt.Sprintf("%s:%d", filename, lineNum)
				hkdb.isWildcard[mapKey] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("knownhosts: %s:%d: %w", filename, lineNum, err)
		}
	}
	return hkdb, nil
}

// HostKeyCallback returns an ssh.HostKeyCallback. This can be used directly in
// ssh.ClientConfig.HostKeyCallback.
func (hkdb *HostKeyDB) HostKeyCallback() ssh.HostKeyCallback {
	// NewDB found no wildcard host patterns: return the callback (which came
	// from x/crypto/ssh/knownhosts) as-is
	if len(hkdb.isWildcard) == 0 {
		return hkdb.callback
	}

	// If we scanned for wildcards and found at least one, return a wrapped
	// callback with extra behavior: if the host lookup found no matches, and the
	// host arg had a non-standard port, re-do the lookup on standard port 22. If
	// that second call returns a *xknownhosts.KeyError, filter down any resulting
	// Want keys to known wildcard entries.
	f := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		trace.SSH.Printf(
			`ssh: wildcard knownhosts for hostname=%s pubkey="%s %s"`,
			hostname, key.Type(), ssh.FingerprintSHA256(key),
		)

		callbackErr := hkdb.callback(hostname, remote, key)
		if callbackErr == nil || isHostKeyChanged(callbackErr) { // hostname has known_host entries as-is
			return callbackErr
		}
		justHost, port, splitErr := net.SplitHostPort(hostname)
		if splitErr != nil || port == "" || port == "22" { // hostname already using standard port
			return callbackErr
		}
		// If we reach here, the port was non-standard and no known_host entries
		// were found for the non-standard port. Try again with standard port.
		if tcpAddr, ok := remote.(*net.TCPAddr); ok && tcpAddr.Port != 22 {
			remote = &net.TCPAddr{
				IP:   tcpAddr.IP,
				Port: 22,
				Zone: tcpAddr.Zone,
			}
		}
		callbackErr = hkdb.callback(justHost+":22", remote, key)
		var keyErr *xknownhosts.KeyError
		if errors.As(callbackErr, &keyErr) && len(keyErr.Want) > 0 {
			wildcardKeys := make([]xknownhosts.KnownKey, 0, len(keyErr.Want))
			for _, wantKey := range keyErr.Want {
				if hkdb.isWildcard[fmt.Sprintf("%s:%d", wantKey.Filename, wantKey.Line)] {
					wildcardKeys = append(wildcardKeys, wantKey)
				}
			}
			callbackErr = &xknownhosts.KeyError{
				Want: wildcardKeys,
			}
		}
		return callbackErr
	}
	return ssh.HostKeyCallback(f)
}

// publicKey wraps ssh.PublicKey with an additional field, to identify
// whether the key corresponds to a certificate authority.
type publicKey struct {
	ssh.PublicKey
	Cert bool
}

// hostKeys returns a slice of known host public keys for the supplied host:port
// found in the known_hosts file(s), or an empty slice if the host is not
// already known. For hosts that have multiple known_hosts entries (for
// different key types), the result will be sorted by known_hosts filename and
// line number. The Cert boolean field of each result entry reports whether the
// key corresponded to a @cert-authority line.
func (hkdb *HostKeyDB) hostKeys(hostWithPort string) (keys []publicKey) {
	var keyErr *xknownhosts.KeyError
	placeholderAddr := &net.TCPAddr{IP: []byte{0, 0, 0, 0}}
	placeholderPubKey := &fakePublicKey{}
	var kkeys []xknownhosts.KnownKey
	callback := hkdb.HostKeyCallback()
	if hkcbErr := callback(hostWithPort, placeholderAddr, placeholderPubKey); errors.As(hkcbErr, &keyErr) {
		kkeys = append(kkeys, keyErr.Want...)
		knownKeyLess := func(i, j int) bool {
			if kkeys[i].Filename < kkeys[j].Filename {
				return true
			}
			return (kkeys[i].Filename == kkeys[j].Filename && kkeys[i].Line < kkeys[j].Line)
		}
		sort.Slice(kkeys, knownKeyLess)
		keys = make([]publicKey, len(kkeys))
		for n := range kkeys {
			keys[n] = publicKey{
				PublicKey: kkeys[n].Key,
			}
			if len(hkdb.isCert) > 0 {
				keys[n].Cert = hkdb.isCert[fmt.Sprintf("%s:%d", kkeys[n].Filename, kkeys[n].Line)]
			}
		}
	}
	return keys
}

// HostKeyAlgorithms returns a slice of host key algorithms for the supplied
// host:port found in the known_hosts file(s), or an empty slice if the host
// is not already known. The result may be used in ssh.ClientConfig's
// HostKeyAlgorithms field, either as-is or after filtering (if you wish to
// ignore or prefer particular algorithms). For hosts that have multiple
// known_hosts entries (of different key types), the result will be sorted by
// known_hosts filename and line number.
// Any @cert-authority lines in the known_hosts file will properly be converted
// to the corresponding ssh.CertAlgo* values.
func (hkdb *HostKeyDB) HostKeyAlgorithms(hostWithPort string) (algos []string) {
	// We ensure that algos never contains duplicates. This is done for robustness
	// even though currently golang.org/x/crypto/ssh/knownhosts never exposes
	// multiple keys of the same type. This way our behavior here is unaffected
	// even if https://github.com/golang/go/issues/28870 is implemented, for
	// example by https://github.com/golang/crypto/pull/254.
	hostKeys := hkdb.hostKeys(hostWithPort)
	seen := make(map[string]struct{}, len(hostKeys))
	addAlgo := func(typ string, cert bool) {
		if cert {
			typ = keyTypeToCertAlgo(typ)
		}
		if _, already := seen[typ]; !already {
			algos = append(algos, typ)
			seen[typ] = struct{}{}
		}
	}
	for _, key := range hostKeys {
		typ := key.Type()
		if typ == ssh.KeyAlgoRSA {
			// KeyAlgoRSASHA256 and KeyAlgoRSASHA512 are only public key algorithms,
			// not public key formats, so they can't appear as a PublicKey.Type.
			// The corresponding PublicKey.Type is KeyAlgoRSA. See RFC 8332, Section 2.
			addAlgo(ssh.KeyAlgoRSASHA512, key.Cert)
			addAlgo(ssh.KeyAlgoRSASHA256, key.Cert)
		}
		addAlgo(typ, key.Cert)
	}
	return algos
}

func keyTypeToCertAlgo(keyType string) string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return ssh.CertAlgoRSAv01
	case ssh.KeyAlgoRSASHA256:
		return ssh.CertAlgoRSASHA256v01
	case ssh.KeyAlgoRSASHA512:
		return ssh.CertAlgoRSASHA512v01
	case ssh.KeyAlgoDSA: //nolint:staticcheck // DSA support needed for legacy SSH servers
		return ssh.CertAlgoDSAv01 //nolint:staticcheck // DSA support needed for legacy SSH servers
	case ssh.KeyAlgoECDSA256:
		return ssh.CertAlgoECDSA256v01
	case ssh.KeyAlgoSKECDSA256:
		return ssh.CertAlgoSKECDSA256v01
	case ssh.KeyAlgoECDSA384:
		return ssh.CertAlgoECDSA384v01
	case ssh.KeyAlgoECDSA521:
		return ssh.CertAlgoECDSA521v01
	case ssh.KeyAlgoED25519:
		return ssh.CertAlgoED25519v01
	case ssh.KeyAlgoSKED25519:
		return ssh.CertAlgoSKED25519v01
	}
	return ""
}

// isHostKeyChanged returns a boolean indicating whether the error indicates
// the host key has changed. It is intended to be called on the error returned
// from invoking a host key callback, to check whether an SSH host is known.
func isHostKeyChanged(err error) bool {
	var keyErr *xknownhosts.KeyError
	return errors.As(err, &keyErr) && len(keyErr.Want) > 0
}

// fakePublicKey is used as part of the work-around for
// https://github.com/golang/go/issues/29286
type fakePublicKey struct{}

func (fakePublicKey) Type() string {
	return "fake-public-key"
}

func (fakePublicKey) Marshal() []byte {
	return []byte("fake public key")
}

func (fakePublicKey) Verify(_ []byte, _ *ssh.Signature) error {
	return errors.New("Verify called on placeholder key")
}
