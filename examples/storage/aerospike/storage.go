package aerospike

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"

	driver "github.com/aerospike/aerospike-client-go"
)

const (
	urlField      = "url"
	referencesSet = "reference"
	configSet     = "config"
)

type Storage struct {
	client *driver.Client
	ns     string
	url    string
}

func NewStorage(client *driver.Client, ns, url string) (*Storage, error) {
	if err := createIndexes(client, ns); err != nil {
		return nil, err
	}

	return &Storage{client: client, ns: ns, url: url}, nil
}

func (s *Storage) NewObject() core.Object {
	return &core.MemoryObject{}
}

func (s *Storage) SetObject(obj core.Object) (core.Hash, error) {
	key, err := s.buildKey(obj.Hash(), obj.Type())
	if err != nil {
		return obj.Hash(), err
	}

	r, err := obj.Reader()
	if err != nil {
		return obj.Hash(), err
	}

	c, err := ioutil.ReadAll(r)
	if err != nil {
		return obj.Hash(), err
	}

	bins := driver.BinMap{
		urlField: s.url,
		"hash":   obj.Hash().String(),
		"type":   obj.Type().String(),
		"blob":   c,
	}

	err = s.client.Put(nil, key, bins)
	return obj.Hash(), err
}

func (s *Storage) Object(t core.ObjectType, h core.Hash) (core.Object, error) {
	key, err := s.buildKey(h, t)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	if rec == nil {
		return nil, core.ErrObjectNotFound
	}

	return objectFromRecord(rec, t)
}

func (s *Storage) IterObjects(t core.ObjectType) (core.ObjectIter, error) {
	stmnt := driver.NewStatement(s.ns, t.String())
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	return &ObjectIter{t, rs.Records}, nil
}

func (s *Storage) buildKey(h core.Hash, t core.ObjectType) (*driver.Key, error) {
	return driver.NewKey(s.ns, t.String(), fmt.Sprintf("%s|%s", s.url, h.String()))
}

type ObjectIter struct {
	t  core.ObjectType
	ch chan *driver.Record
}

func (i *ObjectIter) Next() (core.Object, error) {
	r := <-i.ch
	if r == nil {
		return nil, io.EOF
	}

	return objectFromRecord(r, i.t)
}

func (i *ObjectIter) ForEach(cb func(obj core.Object) error) error {
	for {
		obj, err := i.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if err := cb(obj); err != nil {
			if err == core.ErrStop {
				return nil
			}

			return err
		}
	}
}

func (i *ObjectIter) Close() {}

func objectFromRecord(r *driver.Record, t core.ObjectType) (core.Object, error) {
	content := r.Bins["blob"].([]byte)

	o := &core.MemoryObject{}
	o.SetType(t)
	o.SetSize(int64(len(content)))

	_, err := o.Write(content)
	if err != nil {
		return nil, err
	}

	return o, nil
}

func (s *Storage) SetReference(ref *core.Reference) error {
	key, err := s.buildReferenceKey(ref.Name())
	if err != nil {
		return err
	}

	raw := ref.Strings()
	bins := driver.BinMap{
		urlField: s.url,
		"name":   raw[0],
		"target": raw[1],
	}

	return s.client.Put(nil, key, bins)
}

func (s *Storage) Reference(n core.ReferenceName) (*core.Reference, error) {
	key, err := s.buildReferenceKey(n)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	return core.NewReferenceFromStrings(
		rec.Bins["name"].(string),
		rec.Bins["target"].(string),
	), nil
}

func (s *Storage) buildReferenceKey(n core.ReferenceName) (*driver.Key, error) {
	return driver.NewKey(s.ns, referencesSet, fmt.Sprintf("%s|%s", s.url, n))
}

func (s *Storage) IterReferences() (core.ReferenceIter, error) {
	stmnt := driver.NewStatement(s.ns, referencesSet)
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))
	if err != nil {
		return nil, err
	}

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	var refs []*core.Reference
	for r := range rs.Records {
		refs = append(refs, core.NewReferenceFromStrings(
			r.Bins["name"].(string),
			r.Bins["target"].(string),
		))
	}

	return core.NewReferenceSliceIter(refs), nil
}

func (s *Storage) Config() (*config.Config, error) {
	key, err := s.buildConfigKey()
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	c := &config.Config{}
	return c, json.Unmarshal(rec.Bins["blob"].([]byte), c)
}

func (s *Storage) SetConfig(r *config.Config) error {
	key, err := s.buildConfigKey()
	if err != nil {
		return err
	}

	json, err := json.Marshal(r)
	if err != nil {
		return err
	}

	bins := driver.BinMap{
		urlField: s.url,
		"blob":   json,
	}

	return s.client.Put(nil, key, bins)
}

func (s *Storage) buildConfigKey() (*driver.Key, error) {
	return driver.NewKey(s.ns, configSet, fmt.Sprintf("%s|config", s.url))
}

func createIndexes(c *driver.Client, ns string) error {
	for _, set := range [...]string{
		referencesSet,
		configSet,
		core.BlobObject.String(),
		core.TagObject.String(),
		core.TreeObject.String(),
		core.CommitObject.String(),
	} {
		if err := createIndex(c, ns, set); err != nil {
			return err
		}
	}

	return nil
}

func createIndex(c *driver.Client, ns, set string) error {
	task, err := c.CreateIndex(nil, ns, set, set, urlField, driver.STRING)
	if err != nil {
		if err.Error() == "Index already exists" {
			return nil
		}

		return err
	}

	return <-task.OnComplete()
}
