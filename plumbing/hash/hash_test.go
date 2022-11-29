package hash

import (
	"crypto"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"strings"
	"testing"
)

func TestRegisterHash(t *testing.T) {
	// Reset default hash to avoid side effects.
	defer reset()

	tests := []struct {
		name    string
		hash    crypto.Hash
		new     func() hash.Hash
		wantErr string
	}{
		{
			name: "sha1",
			hash: crypto.SHA1,
			new:  sha1.New,
		},
		{
			name:    "sha1",
			hash:    crypto.SHA1,
			wantErr: "cannot register hash: f is nil",
		},
		{
			name:    "sha512",
			hash:    crypto.SHA512,
			new:     sha512.New,
			wantErr: "unsupported hash function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RegisterHash(tt.hash, tt.new)
			if tt.wantErr == "" && err != nil {
				t.Errorf("unexpected error: %v", err)
			} else if tt.wantErr != "" && err == nil {
				t.Errorf("expected error: %v got: nil", tt.wantErr)
			} else if err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error: %v got: %v", tt.wantErr, err)
			}
		})
	}
}

// Verifies that the SHA1 implementation used is collision-resistant
// by default.
func TestSha1Collision(t *testing.T) {
	defer reset()

	tests := []struct {
		name    string
		content string
		hash    string
		before  func()
	}{
		{
			name:    "sha-mbles-1: with collision detection",
			content: "99040d047fe81780012000ff4b65792069732070617274206f66206120636f6c6c6973696f6e212049742773206120747261702179c61af0afcc054515d9274e7307624b1dc7fb23988bb8de8b575dba7b9eab31c1674b6d974378a827732ff5851c76a2e60772b5a47ce1eac40bb993c12d8c70e24a4f8d5fcdedc1b32c9cf19e31af2429759d42e4dfdb31719f587623ee552939b6dcdc459fca53553b70f87ede30a247ea3af6c759a2f20b320d760db64ff479084fd3ccb3cdd48362d96a9c430617caff6c36c637e53fde28417f626fec54ed7943a46e5f5730f2bb38fb1df6e0090010d00e24ad78bf92641993608e8d158a789f34c46fe1e6027f35a4cbfb827076c50eca0e8b7cca69bb2c2b790259f9bf9570dd8d4437a3115faff7c3cac09ad25266055c27104755178eaeff825a2caa2acfb5de64ce7641dc59a541a9fc9c756756e2e23dc713c8c24c9790aa6b0e38a7f55f14452a1ca2850ddd9562fd9a18ad42496aa97008f74672f68ef461eb88b09933d626b4f918749cc027fddd6c425fc4216835d0134d15285bab2cb784a4f7cbb4fb514d4bf0f6237cf00a9e9f132b9a066e6fd17f6c42987478586ff651af96747fb426b9872b9a88e4063f59bb334cc00650f83a80c42751b71974d300fc2819a2e8f1e32c1b51cb18e6bfc4db9baef675d4aaf5b1574a047f8f6dd2ec153a93412293974d928f88ced9363cfef97ce2e742bf34c96b8ef3875676fea5cca8e5f7dea0bab2413d4de00ee71ee01f162bdb6d1eafd925e6aebaae6a354ef17cf205a404fbdb12fc454d41fdd95cf2459664a2ad032d1da60a73264075d7f1e0d6c1403ae7a0d861df3fe5707188dd5e07d1589b9f8b6630553f8fc352b3e0c27da80bddba4c64020d",
			hash:    "4f3d9be4a472c4dae83c6314aa6c36a064c1fd14",
		},
		{
			name:    "sha-mbles-1: with default SHA1",
			content: "99040d047fe81780012000ff4b65792069732070617274206f66206120636f6c6c6973696f6e212049742773206120747261702179c61af0afcc054515d9274e7307624b1dc7fb23988bb8de8b575dba7b9eab31c1674b6d974378a827732ff5851c76a2e60772b5a47ce1eac40bb993c12d8c70e24a4f8d5fcdedc1b32c9cf19e31af2429759d42e4dfdb31719f587623ee552939b6dcdc459fca53553b70f87ede30a247ea3af6c759a2f20b320d760db64ff479084fd3ccb3cdd48362d96a9c430617caff6c36c637e53fde28417f626fec54ed7943a46e5f5730f2bb38fb1df6e0090010d00e24ad78bf92641993608e8d158a789f34c46fe1e6027f35a4cbfb827076c50eca0e8b7cca69bb2c2b790259f9bf9570dd8d4437a3115faff7c3cac09ad25266055c27104755178eaeff825a2caa2acfb5de64ce7641dc59a541a9fc9c756756e2e23dc713c8c24c9790aa6b0e38a7f55f14452a1ca2850ddd9562fd9a18ad42496aa97008f74672f68ef461eb88b09933d626b4f918749cc027fddd6c425fc4216835d0134d15285bab2cb784a4f7cbb4fb514d4bf0f6237cf00a9e9f132b9a066e6fd17f6c42987478586ff651af96747fb426b9872b9a88e4063f59bb334cc00650f83a80c42751b71974d300fc2819a2e8f1e32c1b51cb18e6bfc4db9baef675d4aaf5b1574a047f8f6dd2ec153a93412293974d928f88ced9363cfef97ce2e742bf34c96b8ef3875676fea5cca8e5f7dea0bab2413d4de00ee71ee01f162bdb6d1eafd925e6aebaae6a354ef17cf205a404fbdb12fc454d41fdd95cf2459664a2ad032d1da60a73264075d7f1e0d6c1403ae7a0d861df3fe5707188dd5e07d1589b9f8b6630553f8fc352b3e0c27da80bddba4c64020d",
			hash:    "8ac60ba76f1999a1ab70223f225aefdc78d4ddc0",
			before: func() {
				RegisterHash(crypto.SHA1, sha1.New)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.before != nil {
				tt.before()
			}

			h := New(crypto.SHA1)
			data, err := hex.DecodeString(tt.content)
			if err != nil {
				t.Fatal(err)
			}

			h.Reset()
			h.Write(data)
			sum := h.Sum(nil)
			got := hex.EncodeToString(sum)

			if tt.hash != got {
				t.Errorf("\n   got: %q\nwanted: %q", got, tt.hash)
			}
		})
	}
}
