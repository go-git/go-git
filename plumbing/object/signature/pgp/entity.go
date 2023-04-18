package pgp

import (
	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/go-git/go-git/v5/plumbing/object/signature"
)

// EntityType is the PGP Entity type. It can be used to detect if a
// signature.Entity is of type PGP.
const EntityType signature.EntityType = "PGP"

// Entity is the PGP entity that signed a signature.VerifiableObject.
// Using the Entity method, you can get the underlying openpgp.Entity.
type Entity struct {
	entity *openpgp.Entity
}

// Canonical returns the canonical identifier of the Entity. Which equals to
// the primary key ID of the openpgp.Entity.
func (s *Entity) Canonical() string {
	return s.entity.PrimaryKey.KeyIdString()
}

// Type returns the EntityType of the Entity.
func (s *Entity) Type() signature.EntityType {
	return EntityType
}

// Concrete returns the underlying concrete type of the Entity. In this case
// a pointer to an openpgp.Entity.
func (s *Entity) Concrete() interface{} {
	return s.entity
}
