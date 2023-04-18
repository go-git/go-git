package ssh

import (
	"golang.org/x/crypto/ssh"

	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// EntityType is the SSH Entity type. It can be used to detect if a
// signature.Entity is of type SSH.
const EntityType signature.EntityType = "SSH"

// Entity is the SSH entity that signed a signature.VerifiableObject.
// Using the Entity method, you can get the underlying ssh.PublicKey.
type Entity struct {
	publicKey ssh.PublicKey
}

// Canonical returns the canonical identifier of the Entity. Which equals to
// a marshalled authorized key (as in a known_hosts file).
func (s *Entity) Canonical() string {
	return string(ssh.MarshalAuthorizedKey(s.publicKey))
}

// Type returns the EntityType of the Entity.
func (s *Entity) Type() signature.EntityType {
	return EntityType
}

// Concrete returns the underlying concrete type of the Entity. In this case
// a pointer to an ssh.PublicKey.
func (s *Entity) Concrete() interface{} {
	return s.publicKey
}
