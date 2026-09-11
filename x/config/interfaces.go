package config

// Marshaler is implemented by types that can marshal themselves into a
// Git config string value.
type Marshaler interface {
	MarshalGitConfig() (string, error)
}

// Unmarshaler is implemented by types that can unmarshal a Git config
// string value into themselves.
type Unmarshaler interface {
	UnmarshalGitConfig([]byte) error
}
