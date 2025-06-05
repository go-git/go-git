package sign

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/ssh"
)

type SSHSignerSuite struct {
	suite.Suite
}

func TestSSHSignerSuite(t *testing.T) {
	suite.Run(t, new(SSHSignerSuite))
}

func (s *SSHSignerSuite) TestSSHSigner() {

	signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
	s.Require().NoError(err)

	sshSigner := SSHSigner{signer: signer}

	// Test signing some data
	message := []byte("Hello, World!")
	signature, err := sshSigner.Sign(bytes.NewReader(message))
	s.Require().NoError(err)

	expectedSig := `-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAgTqqV4ocwj1+nH7LQM5+y60PHx8
RrNfUjQMtP0VeBktkAAAADZ2l0AAAAAAAAAAZzaGE1MTIAAABTAAAAC3NzaC1lZDI1NTE5
AAAAQM+NngDVePFH29Oh+OUYJU4gg/VtxHeNsS60Bd9Pl8yBDnQL7bmFhUfmxxgyl18fP2
x7yylBWNmhl6wp+s7zug0=
-----END SSH SIGNATURE-----
`

	s.Equal(expectedSig, string(signature))
}

const testPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBOqpXihzCPX6cfstAzn7LrQ8fHxGs19SNAy0/RV4GS2QAAAJg17vtQNe77
UAAAAAtzc2gtZWQyNTUxOQAAACBOqpXihzCPX6cfstAzn7LrQ8fHxGs19SNAy0/RV4GS2Q
AAAECq+2ykjxB1YMskT26siDzi5Ze3GkCuTwMHuQfxkxSfB06qleKHMI9fpx+y0DOfsutD
x8fEazX1I0DLT9FXgZLZAAAAEHRlc3RAZXhhbXBsZS5jb20BAgMEBQ==
-----END OPENSSH PRIVATE KEY-----
`
