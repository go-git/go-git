package signature

// EntityType is the type of Entity. For example, PGP or SSH.
type EntityType string

// Entity is the entity which signed a VerifiableObject.
type Entity interface {
	// Canonical returns the canonical identifier of the Entity. For example, a
	// PGP key ID or an SSH public key fingerprint.
	Canonical() string
	// Type returns the type of the Entity. It can be useful to know if an
	// Entity is of a certain type before attempting to cast Entity to an
	// underlying type using Concrete.
	Type() EntityType
	// Concrete returns the underlying concrete type of the Entity.
	// For example, a PGP entity or an SSH public key. This can be useful for
	// extracting other details about the Entity than just Canonical.
	Concrete() interface{}
}
