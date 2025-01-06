package hash

var (
	zeroSHA1   = SHA1Hash{}
	zeroSHA256 = SHA256Hash{}
)

func Zero() SHA1Hash {
	return zeroSHA1
}

func ZeroSHA256() SHA256Hash {
	return zeroSHA256
}
