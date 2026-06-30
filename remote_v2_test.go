package git

import (
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/protocol"
	"github.com/go-git/go-git/v6/storage/memory"
)

func (s *RemoteSuite) TestTransportProtocolDefault() {
	r := NewRemote(nil, &config.RemoteConfig{Name: "foo", URLs: []string{"https://example.com/foo.git"}})
	s.Equal(config.DefaultProtocolVersion, r.transportProtocol())
}

func (s *RemoteSuite) TestTransportProtocolFromConfig() {
	st := memory.NewStorage()
	cfg, err := st.Config()
	s.Require().NoError(err)
	cfg.Protocol.Version = protocol.V2
	s.Require().NoError(st.SetConfig(cfg))

	r := NewRemote(st, &config.RemoteConfig{Name: "foo", URLs: []string{"https://example.com/foo.git"}})
	s.Equal(protocol.V2, r.transportProtocol())
}
