package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type OptionSuite struct {
	suite.Suite
}

func TestOptionSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(OptionSuite))
}

func (s *OptionSuite) TestOptions_Has() {
	o := Options{
		&Option{"k", "v"},
		&Option{"ok", "v1"},
		&Option{"K", "v2"},
	}
	s.True(o.Has("k"))
	s.True(o.Has("K"))
	s.True(o.Has("ok"))
	s.False(o.Has("unexistant"))

	o = Options{}
	s.False(o.Has("k"))
}

func (s *OptionSuite) TestOptions_GetAll() {
	o := Options{
		&Option{"k", "v"},
		&Option{"ok", "v1"},
		&Option{"K", "v2"},
	}
	s.Equal([]string{"v", "v2"}, o.GetAll("k"))
	s.Equal([]string{"v", "v2"}, o.GetAll("K"))
	s.Equal([]string{"v1"}, o.GetAll("ok"))
	s.Equal([]string{}, o.GetAll("unexistant"))

	o = Options{}
	s.Equal([]string{}, o.GetAll("k"))
}

func (s *OptionSuite) TestOption_IsKey() {
	s.True((&Option{Key: "key"}).IsKey("key"))
	s.True((&Option{Key: "key"}).IsKey("KEY"))
	s.True((&Option{Key: "KEY"}).IsKey("key"))
	s.False((&Option{Key: "key"}).IsKey("other"))
	s.False((&Option{Key: "key"}).IsKey(""))
	s.False((&Option{Key: ""}).IsKey("key"))
}
