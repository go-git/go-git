package object

// TrustLevel represents the trust level of a signing key.
// The levels follow Git's trust model, from lowest to highest.
type TrustLevel int8

const (
	// TrustUndefined indicates the trust level is not set or unknown.
	TrustUndefined TrustLevel = iota
	// TrustNever indicates the key should never be trusted.
	TrustNever
	// TrustMarginal indicates marginal trust in the key.
	TrustMarginal
	// TrustFull indicates full trust in the key.
	TrustFull
	// TrustUltimate indicates ultimate trust (typically for own keys).
	TrustUltimate
)

// String returns the string representation of the trust level.
func (t TrustLevel) String() string {
	switch t {
	case TrustNever:
		return "never"
	case TrustMarginal:
		return "marginal"
	case TrustFull:
		return "full"
	case TrustUltimate:
		return "ultimate"
	default:
		return "undefined"
	}
}

// AtLeast returns true if this trust level meets or exceeds the required level.
func (t TrustLevel) AtLeast(required TrustLevel) bool {
	return t >= required
}
