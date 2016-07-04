package packfile

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"

	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/storage/memory"

	. "gopkg.in/check.v1"
)

type ReadRecallerImplSuite struct{}

var _ = Suite(&ReadRecallerImplSuite{})

type implFn func([]byte) ReadRecaller

func newStream(data []byte) ReadRecaller {
	buf := bytes.NewBuffer(data)
	return NewStream(buf)
}

func newSeekable(data []byte) ReadRecaller {
	buf := bytes.NewReader(data)
	return NewSeekable(buf)
}

func (s *ReadRecallerImplSuite) TestRead(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		data := []byte{0, 1, 2, 3, 4, 5, 7, 8, 9, 10}
		sr := impl.newFn(data)
		all := make([]byte, 0, len(data))

		for len(all) < len(data) {
			tmp := make([]byte, 3)
			nr, err := sr.Read(tmp)
			c.Assert(err, IsNil, com)
			all = append(all, tmp[:nr]...)
		}
		c.Assert(data, DeepEquals, all, com)
	}
}

func (s *ReadRecallerImplSuite) TestReadbyte(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		data := []byte{0, 1, 2, 3, 4, 5, 7, 8, 9, 10}
		sr := impl.newFn(data)
		all := make([]byte, 0, len(data))

		for len(all) < len(data) {
			b, err := sr.ReadByte()
			c.Assert(err, IsNil, com)
			all = append(all, b)
		}
		c.Assert(data, DeepEquals, all, com)
	}
}

func (s *ReadRecallerImplSuite) TestOffsetWithRead(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		data := []byte{0, 1, 2, 3, 4, 5, 7, 8, 9, 10}
		sr := impl.newFn(data)
		all := make([]byte, 0, len(data))

		for len(all) < len(data) {
			tmp := make([]byte, 3)
			nr, err := sr.Read(tmp)
			c.Assert(err, IsNil, com)
			all = append(all, tmp[:nr]...)

			off, err := sr.Offset()
			c.Assert(err, IsNil, com)
			c.Assert(off, Equals, int64(len(all)), com)
		}
	}
}

func (s *ReadRecallerImplSuite) TestOffsetWithReadByte(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		data := []byte{0, 1, 2, 3, 4, 5, 7, 8, 9, 10}
		sr := impl.newFn(data)
		all := make([]byte, 0, len(data))

		for len(all) < len(data) {
			b, err := sr.ReadByte()
			c.Assert(err, IsNil, com)
			all = append(all, b)

			off, err := sr.Offset()
			c.Assert(err, IsNil, com)
			c.Assert(off, Equals, int64(len(all)), com)
		}
	}
}

func (s *ReadRecallerImplSuite) TestRememberRecall(c *C) {
	packfile := "fixtures/spinnaker-spinnaker.pack"
	f, err := os.Open(packfile)
	c.Assert(err, IsNil)
	defer func() {
		err = f.Close()
		c.Assert(err, IsNil)
	}()

	data, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)

	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		sr := impl.newFn(data)
		for i, test := range [...]struct {
			off    int64
			obj    core.Object
			err    string // error regexp
			ignore string // ignore this test for this implementation
		}{
			{
				off: 12,
				obj: newObj(core.CommitObject, []byte("tree 44a1cdf21c791867c51caad8f1b77e6baee6f462\nparent 87fe6e7c6b1b89519fe3a03a8961c5aa14d4cc68\nparent 9244ee648182b91a63d8cc4cbe4b9ac2a27c0492\nauthor Matt Duftler <duftler@google.com> 1448290941 -0500\ncommitter Matt Duftler <duftler@google.com> 1448290941 -0500\n\nMerge pull request #615 from ewiseblatt/create_dev\n\nPreserve original credentials of spinnaker-local.yml when transforming it.")),
			}, {
				off: 3037,
				obj: newObj(core.TagObject, []byte("object e0005f50e22140def60260960b21667f1fdfff80\ntype commit\ntag v0.10.0\ntagger cfieber <cfieber@netflix.com> 1447687536 -0800\n\nRelease of 0.10.0\n\n- e0005f50e22140def60260960b21667f1fdfff80: Merge pull request #553 from ewiseblatt/rendezvous\n- e1a2b26b784179e6903a7ae967c037c721899eba: Wait for cassandra before starting spinnaker\n- c756e09461d071e98b8660818cf42d90c90f2854: Merge pull request #552 from duftler/google-c2d-tweaks\n- 0777fadf4ca6f458d7071de414f9bd5417911037: Fix incorrect config prop names:   s/SPINNAKER_GOOGLE_PROJECT_DEFAULT_REGION/SPINNAKER_GOOGLE_DEFAULT_REGION   s/SPINNAKER_GOOGLE_PROJECT_DEFAULT_ZONE/SPINNAKER_GOOGLE_DEFAULT_ZONE Hardcode profile name in generated ~/.aws/credentials to [default]. Restart all of spinnaker after updating cassandra and reconfiguring spinnaker, instead of just restarting clouddriver.\n- d8d031c1ac45801074418c43424a6f2c0dff642c: Merge pull request #551 from kenzanmedia/fixGroup\n- 626d23075f9e92aad19015f2964c95d45f41fa3a: Put in correct block for public image. Delineate cloud provider.\n")),
			}, {
				off: 157625,
				obj: newObj(core.BlobObject, []byte(".gradle\nbuild/\n*.iml\n.idea\n*.pyc\n*~\n#*\nconfig/spinnaker-local.yml\n.DS_Store\npacker/ami_table.md\npacker/ami_table.json\npacker/example_output.txt")),
			}, {
				off: 1234,
				obj: newObj(core.BlobObject, []byte(".gradle\nbuild/\n*.iml\n.idea\n*.pyc\n*~\n#*\nconfig/spinnaker-local.yml\n.DS_Store\npacker/ami_table.md\npacker/ami_table.json\npacker/example_output.txt")),
				err: "duplicated object: with hash .*",
			}, {
				off:    3037,
				obj:    newObj(core.BlobObject, []byte("")),
				err:    "duplicated object: with offset 3037",
				ignore: "seekable",
				// seekable can not check if the offset has already been added
				// for performance reasons.
			},
		} {
			if test.ignore == impl.id {
				continue
			}
			com := Commentf("subtest %d) implementation %s", i, impl.id)

			err := sr.Remember(test.off, test.obj)
			if test.err != "" {
				c.Assert(err, ErrorMatches, test.err, com)
				continue
			}
			c.Assert(err, IsNil, com)

			result, err := sr.RecallByHash(test.obj.Hash())
			c.Assert(err, IsNil, com)
			c.Assert(result, DeepEquals, test.obj, com)

			result, err = sr.RecallByOffset(test.off)
			c.Assert(err, IsNil, com)
			c.Assert(result, DeepEquals, test.obj, com)
		}
	}
}

func newObj(typ core.ObjectType, cont []byte) core.Object {
	return memory.NewObject(typ, int64(len(cont)), cont)
}

func (s *ReadRecallerImplSuite) TestRecallByHashErrors(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		sr := impl.newFn([]byte{})
		obj := newObj(core.CommitObject, []byte{})

		_, err := sr.RecallByHash(obj.Hash())
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)

		err = rememberSomeObjects(sr)
		c.Assert(err, IsNil)

		_, err = sr.RecallByHash(obj.Hash())
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
	}
}

func (s *ReadRecallerImplSuite) TestRecallByOffsetErrors(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		// seekalbe allways recall every object in the packfile
	} {
		com := Commentf("implementation %s", impl.id)
		sr := impl.newFn([]byte{})

		_, err := sr.RecallByOffset(15)
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)

		err = rememberSomeObjects(sr)
		c.Assert(err, IsNil)

		_, err = sr.RecallByOffset(15)
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
	}
}

func rememberSomeObjects(sr ReadRecaller) error {
	for i, init := range [...]struct {
		off int64
		obj core.Object
	}{
		{off: 0, obj: newObj(core.CommitObject, []byte{'a'})},  // 93114cce67ec23976d15199514399203f69cc676
		{off: 10, obj: newObj(core.CommitObject, []byte{'b'})}, // 2bb767097e479f668f0ebdabe88df11337bd8f19
		{off: 20, obj: newObj(core.CommitObject, []byte{'c'})}, // 2f8096005677370e6446541a50e074299d43d468
	} {
		err := sr.Remember(init.off, init.obj)
		if err != nil {
			return fmt.Errorf("cannot ask StreamReader to Remember item %d", i)
		}
	}

	return nil
}

func (s *ReadRecallerImplSuite) TestForgetAll(c *C) {
	for _, impl := range []struct {
		id    string
		newFn implFn
	}{
		{id: "stream", newFn: newStream},
		{id: "seekable", newFn: newSeekable},
	} {
		com := Commentf("implementation %s", impl.id)
		sr := impl.newFn([]byte{})

		err := rememberSomeObjects(sr)
		c.Assert(err, IsNil)

		sr.ForgetAll()

		if impl.id != "seekable" { // for efficiency, seekable always finds objects by offset
			_, err = sr.RecallByOffset(0)
			c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
			_, err = sr.RecallByOffset(10)
			c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
			_, err = sr.RecallByOffset(20)
			c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
		}
		_, err = sr.RecallByHash(core.NewHash("93114cce67ec23976d15199514399203f69cc676"))
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
		_, err = sr.RecallByHash(core.NewHash("2bb767097e479f668f0ebdabe88df11337bd8f19"))
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
		_, err = sr.RecallByHash(core.NewHash("2f8096005677370e6446541a50e074299d43d468"))
		c.Assert(err, ErrorMatches, ErrCannotRecall.Error()+".*", com)
	}
}
