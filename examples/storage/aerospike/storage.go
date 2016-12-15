package aerospike

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"

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

func (s *Storage) NewEncodedObject() plumbing.EncodedObject {
	return &plumbing.MemoryObject{}
}

func (s *Storage) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
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

func (s *Storage) EncodedObject(t plumbing.ObjectType, h plumbing.Hash) (plumbing.EncodedObject, error) {
	key, err := s.buildKey(h, t)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	if rec == nil {
		return nil, plumbing.ErrObjectNotFound
	}

	return objectFromRecord(rec, t)
}

func (s *Storage) IterEncodedObjects(t plumbing.ObjectType) (storer.EncodedObjectIter, error) {
	stmnt := driver.NewStatement(s.ns, t.String())
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	return &EncodedObjectIter{t, rs.Records}, nil
}

func (s *Storage) buildKey(h plumbing.Hash, t plumbing.ObjectType) (*driver.Key, error) {
	return driver.NewKey(s.ns, t.String(), fmt.Sprintf("%s|%s", s.url, h.String()))
}

type EncodedObjectIter struct {
	t  plumbing.ObjectType
	ch chan *driver.Record
}

func (i *EncodedObjectIter) Next() (plumbing.EncodedObject, error) {
	r := <-i.ch
	if r == nil {
		return nil, io.EOF
	}

	return objectFromRecord(r, i.t)
}

func (i *EncodedObjectIter) ForEach(cb func(obj plumbing.EncodedObject) error) error {
	for {
		obj, err := i.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		if err := cb(obj); err != nil {
			if err == storer.ErrStop {
				return nil
			}

			return err
		}
	}
}

func (i *EncodedObjectIter) Close() {}

func objectFromRecord(r *driver.Record, t plumbing.ObjectType) (plumbing.EncodedObject, error) {
	content := r.Bins["blob"].([]byte)

	o := &plumbing.MemoryObject{}
	o.SetType(t)
	o.SetSize(int64(len(content)))

	_, err := o.Write(content)
	if err != nil {
		return nil, err
	}

	return o, nil
}

func (s *Storage) SetReference(ref *plumbing.Reference) error {
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

func (s *Storage) Reference(n plumbing.ReferenceName) (*plumbing.Reference, error) {
	key, err := s.buildReferenceKey(n)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	return plumbing.NewReferenceFromStrings(
		rec.Bins["name"].(string),
		rec.Bins["target"].(string),
	), nil
}

func (s *Storage) buildReferenceKey(n plumbing.ReferenceName) (*driver.Key, error) {
	return driver.NewKey(s.ns, referencesSet, fmt.Sprintf("%s|%s", s.url, n))
}

func (s *Storage) IterReferences() (storer.ReferenceIter, error) {
	stmnt := driver.NewStatement(s.ns, referencesSet)
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))
	if err != nil {
		return nil, err
	}

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	var refs []*plumbing.Reference
	for r := range rs.Records {
		refs = append(refs, plumbing.NewReferenceFromStrings(
			r.Bins["name"].(string),
			r.Bins["target"].(string),
		))
	}

	return storer.NewReferenceSliceIter(refs), nil
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

func (s *Storage) Shallow() ([]plumbing.Hash, error) {
	key, err := s.buildShallowKey()
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	var h []plumbing.Hash
	return h, json.Unmarshal(rec.Bins["blob"].([]byte), h)
}

func (s *Storage) SetShallow(hash []plumbing.Hash) error {
	key, err := s.buildShallowKey()
	if err != nil {
		return err
	}

	json, err := json.Marshal(hash)
	if err != nil {
		return err
	}

	bins := driver.BinMap{
		urlField: s.url,
		"blob":   json,
	}

	return s.client.Put(nil, key, bins)
}

func (s *Storage) buildShallowKey() (*driver.Key, error) {
	return driver.NewKey(s.ns, configSet, fmt.Sprintf("%s|config", s.url))
}

func createIndexes(c *driver.Client, ns string) error {
	for _, set := range [...]string{
		referencesSet,
		configSet,
		plumbing.BlobObject.String(),
		plumbing.TagObject.String(),
		plumbing.TreeObject.String(),
		plumbing.CommitObject.String(),
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
