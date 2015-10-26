package git

import . "gopkg.in/check.v1"

type SuiteRepository struct{}

var _ = Suite(&SuiteRepository{})

func (s *SuiteRepository) TestPull(c *C) {
	r, err := NewRepository(RepositoryFixture)
	c.Assert(err, IsNil)
	c.Assert(r.Pull("origin", "refs/heads/master"), IsNil)

	//fmt.Println(r.Storage)
}
