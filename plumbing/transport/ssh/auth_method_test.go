package ssh

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-billy/v6/util"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

func TestSuiteCommon(t *testing.T) {
	suite.Run(t, new(SuiteCommon))
}

type (
	SuiteCommon struct{ suite.Suite }

	mockKnownHosts         struct{}
	mockKnownHostsWithCert struct{}
)

// knownHostsMock defines the interface for known hosts mock types.
type knownHostsMock interface {
	fmt.Stringer
	knownHosts() []byte
	Algorithms() []string
}

func (mockKnownHosts) host() string { return "github.com" }
func (mockKnownHosts) knownHosts() []byte {
	return []byte(`github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==`)
}
func (mockKnownHosts) Network() string { return "tcp" }
func (mockKnownHosts) String() string  { return "github.com:22" }
func (mockKnownHosts) Algorithms() []string {
	return []string{ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512}
}

func (mockKnownHostsWithCert) knownHosts() []byte {
	return []byte(`@cert-authority github.com ssh-rsa AAAAB3NzaC1yc2EAAAABIwAAAQEAq2A7hRGmdnm9tUDbO9IDSwBK6TbQa+PXYPCPy6rbTrTtw7PHkccKrpp0yVhp5HdEIcKr6pLlVDBfOLX9QUsyCOV0wzfjIJNlGEYsdlLJizHhbn2mUjvSAHQqZETYP81eFzLQNnPHt4EVVUh7VfDESU84KezmD5QlWpXLmvU31/yMf+Se8xhHTvKSCZIFImWwoG6mbUoWf9nzpIoaSjB+weqqUUmpaaasXVal72J+UX2B+2RPW3RcT0eOzQgqlJL3RKrTJvdsjE3JEAvGq3lGHSZXy28G3skua2SmVi/w4yCE6gbODqnTWlg7+wC604ydGXA8VJiS5ap43JXiUFFAaQ==`)
}
func (mockKnownHostsWithCert) Network() string { return "tcp" }
func (mockKnownHostsWithCert) String() string  { return "github.com:22" }
func (mockKnownHostsWithCert) Algorithms() []string {
	return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01}
}

func (s *SuiteCommon) TestKeyboardInteractiveName() {
	a := &KeyboardInteractive{
		User:      "test",
		Challenge: nil,
	}
	s.Equal(KeyboardInteractiveName, a.Name())
}

func (s *SuiteCommon) TestKeyboardInteractiveString() {
	a := &KeyboardInteractive{
		User:      "test",
		Challenge: nil,
	}
	s.Equal(fmt.Sprintf("user: test, name: %s", KeyboardInteractiveName), a.String())
}

func (s *SuiteCommon) TestPasswordName() {
	a := &Password{
		User:     "test",
		Password: "",
	}
	s.Equal(PasswordName, a.Name())
}

func (s *SuiteCommon) TestPasswordString() {
	a := &Password{
		User:     "test",
		Password: "",
	}
	s.Equal(fmt.Sprintf("user: test, name: %s", PasswordName), a.String())
}

func (s *SuiteCommon) TestPasswordCallbackName() {
	a := &PasswordCallback{
		User:     "test",
		Callback: nil,
	}
	s.Equal(PasswordCallbackName, a.Name())
}

func (s *SuiteCommon) TestPasswordCallbackString() {
	a := &PasswordCallback{
		User:     "test",
		Callback: nil,
	}
	s.Equal(fmt.Sprintf("user: test, name: %s", PasswordCallbackName), a.String())
}

func (s *SuiteCommon) TestPublicKeysName() {
	a := &PublicKeys{
		User:   "test",
		Signer: nil,
	}
	s.Equal(PublicKeysName, a.Name())
}

func (s *SuiteCommon) TestPublicKeysString() {
	a := &PublicKeys{
		User:   "test",
		Signer: nil,
	}
	s.Equal(fmt.Sprintf("user: test, name: %s", PublicKeysName), a.String())
}

func (s *SuiteCommon) TestPublicKeysCallbackName() {
	a := &PublicKeysCallback{
		User:     "test",
		Callback: nil,
	}
	s.Equal(PublicKeysCallbackName, a.Name())
}

func (s *SuiteCommon) TestPublicKeysCallbackString() {
	a := &PublicKeysCallback{
		User:     "test",
		Callback: nil,
	}
	s.Equal(fmt.Sprintf("user: test, name: %s", PublicKeysCallbackName), a.String())
}

func (s *SuiteCommon) TestNewSSHAgentAuth() {
	if runtime.GOOS == "js" {
		s.T().Skip("tcp connections are not available in wasm")
	}

	if os.Getenv("SSH_AUTH_SOCK") == "" {
		s.T().Skip("SSH_AUTH_SOCK or SSH_TEST_PRIVATE_KEY are required")
	}

	auth, err := NewSSHAgentAuth("foo")
	s.NoError(err)
	s.NotNil(auth)
}

func (s *SuiteCommon) TestNewSSHAgentAuthNoAgent() {
	addr := os.Getenv("SSH_AUTH_SOCK")
	err := os.Unsetenv("SSH_AUTH_SOCK")
	s.NoError(err)

	defer func() {
		err := os.Setenv("SSH_AUTH_SOCK", addr)
		s.NoError(err)
	}()

	k, err := NewSSHAgentAuth("foo")
	s.Nil(k)
	s.Regexp(".*SSH_AUTH_SOCK.*|.*SSH agent .* not detect.*", err.Error())
}

func (s *SuiteCommon) TestNewPublicKeys() {
	auth, err := NewPublicKeys("foo", testdata.PEMBytes["rsa"], "")
	s.NoError(err)
	s.NotNil(auth)
}

func (s *SuiteCommon) TestNewPublicKeysWithEncryptedPEM() {
	f := testdata.PEMEncryptedKeys[0]
	auth, err := NewPublicKeys("foo", f.PEMBytes, f.EncryptionKey)
	s.NoError(err)
	s.NotNil(auth)
}

func (s *SuiteCommon) TestNewPublicKeysWithEncryptedEd25519PEM() {
	f := testdata.PEMEncryptedKeys[2]
	auth, err := NewPublicKeys("foo", f.PEMBytes, f.EncryptionKey)
	s.NoError(err)
	s.NotNil(auth)
}

func (s *SuiteCommon) TestNewPublicKeysFromFile() {
	if runtime.GOOS == "js" {
		s.T().Skip("not available in wasm")
	}

	f, err := util.TempFile(osfs.Default, "", "ssh-test")
	s.NoError(err)
	_, err = f.Write(testdata.PEMBytes["rsa"])
	s.NoError(err)
	s.NoError(f.Close())
	defer osfs.Default.Remove(f.Name())

	auth, err := NewPublicKeysFromFile("foo", f.Name(), "")
	s.NoError(err)
	s.NotNil(auth)
}

func (s *SuiteCommon) TestNewPublicKeysWithInvalidPEM() {
	auth, err := NewPublicKeys("foo", []byte("bar"), "")
	s.Error(err)
	s.Nil(auth)
}

func (s *SuiteCommon) TestNewKnownHostsCallback() {
	if runtime.GOOS == "js" {
		s.T().Skip("not available in wasm")
	}

	mock := mockKnownHosts{}

	f, err := util.TempFile(osfs.Default, "", "known-hosts")
	s.NoError(err)

	_, err = f.Write(mock.knownHosts())
	s.NoError(err)

	err = f.Close()
	s.NoError(err)

	defer util.RemoveAll(osfs.Default, f.Name())

	f, err = osfs.Default.Open(f.Name())
	s.NoError(err)

	defer f.Close()

	var hostKey ssh.PublicKey
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		if len(fields) != 3 {
			continue
		}
		if strings.Contains(fields[0], mock.host()) {
			var err error
			hostKey, _, _, _, err = ssh.ParseAuthorizedKey(scanner.Bytes())
			if err != nil {
				s.T().Fatalf("error parsing %q: %v", fields[2], err)
			}
			break
		}
	}
	if hostKey == nil {
		s.T().Fatalf("no hostkey for %s", mock.host())
	}

	clb, err := NewKnownHostsCallback(f.Name())
	s.NoError(err)

	err = clb(mock.String(), mock, hostKey)
	s.NoError(err)
}

func (s *SuiteCommon) testNewKnownHostsDb(mock knownHostsMock) {
	f, err := util.TempFile(osfs.Default, "", "known-hosts")
	s.NoError(err)

	_, err = f.Write(mock.knownHosts())
	s.NoError(err)

	err = f.Close()
	s.NoError(err)

	defer util.RemoveAll(osfs.Default, f.Name())

	f, err = osfs.Default.Open(f.Name())
	s.NoError(err)

	defer f.Close()

	db, err := newKnownHostsDb(f.Name())
	s.NoError(err)

	algos := db.HostKeyAlgorithms(mock.String())
	s.Len(algos, len(mock.Algorithms()))

	for _, algorithm := range mock.Algorithms() {
		if !slices.Contains(algos, algorithm) {
			s.T().Error("algos does not contain ", algorithm)
		}
	}
}

func (s *SuiteCommon) TestNewKnownHostsDbWithoutCert() {
	if runtime.GOOS == "js" {
		s.T().Skip("not available in wasm")
	}
	s.testNewKnownHostsDb(mockKnownHosts{})
}

func (s *SuiteCommon) TestNewKnownHostsDbWithCert() {
	if runtime.GOOS == "js" {
		s.T().Skip("not available in wasm")
	}
	s.testNewKnownHostsDb(mockKnownHostsWithCert{})
}
