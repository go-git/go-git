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
	remotesSet    = "remote"
)

type Storage struct {
	client *driver.Client
	ns     string
	url    string

	os *ObjectStorage
	rs *ReferenceStorage
	cs *ConfigStorage
}

func NewStorage(client *driver.Client, ns, url string) (*Storage, error) {
	if err := createIndexes(client, ns); err != nil {
		return nil, err
	}

	return &Storage{client: client, ns: ns, url: url}, nil
}

func (s *Storage) ObjectStorage() core.ObjectStorage {
	if s.os == nil {
		s.os = &ObjectStorage{s.client, s.ns, s.url}
	}

	return s.os
}

func (s *Storage) ReferenceStorage() core.ReferenceStorage {
	if s.rs == nil {
		s.rs = &ReferenceStorage{s.client, s.ns, s.url}
	}

	return s.rs
}

func (s *Storage) ConfigStorage() config.ConfigStorage {
	if s.cs == nil {
		s.cs = &ConfigStorage{s.client, s.ns, s.url}
	}

	return s.cs
}

type ObjectStorage struct {
	client *driver.Client
	ns     string
	url    string
}

func (s *ObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

// Writer method not supported, this method is optional to implemented.
func (s *ObjectStorage) Writer() (io.WriteCloser, error) {
	return nil, core.ErrNotImplemented
}

func (s *ObjectStorage) Set(obj core.Object) (core.Hash, error) {
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

func (s *ObjectStorage) Get(t core.ObjectType, h core.Hash) (core.Object, error) {
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

func (s *ObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	stmnt := driver.NewStatement(s.ns, t.String())
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	return &ObjectIter{t, rs.Records}, nil
}

func (s *ObjectStorage) buildKey(h core.Hash, t core.ObjectType) (*driver.Key, error) {
	return driver.NewKey(s.ns, t.String(), fmt.Sprintf("%s|%s", s.url, h.String()))
}

func (s *ObjectStorage) Begin() core.TxObjectStorage {
	return &TxObjectStorage{Storage: s}
}

type TxObjectStorage struct {
	Storage *ObjectStorage
}

func (tx *TxObjectStorage) Set(obj core.Object) (core.Hash, error) {
	return tx.Storage.Set(obj)
}

func (tx *TxObjectStorage) Commit() error {
	return nil
}

func (tx *TxObjectStorage) Rollback() error {
	return nil
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

type ReferenceStorage struct {
	client *driver.Client
	ns     string
	url    string
}

// Set stores a reference.
func (s *ReferenceStorage) Set(ref *core.Reference) error {
	key, err := s.buildKey(ref.Name())
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

// Get returns a stored reference with the given name
func (s *ReferenceStorage) Get(n core.ReferenceName) (*core.Reference, error) {
	key, err := s.buildKey(n)
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

func (s *ReferenceStorage) buildKey(n core.ReferenceName) (*driver.Key, error) {
	return driver.NewKey(s.ns, referencesSet, fmt.Sprintf("%s|%s", s.url, n))
}

// Iter returns a core.ReferenceIter
func (s *ReferenceStorage) Iter() (core.ReferenceIter, error) {
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

type ConfigStorage struct {
	client *driver.Client
	ns     string
	url    string
}

func (s *ConfigStorage) Remote(name string) (*config.RemoteConfig, error) {
	key, err := s.buildRemoteKey(name)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	return remoteFromRecord(rec)
}

func remoteFromRecord(r *driver.Record) (*config.RemoteConfig, error) {
	content := r.Bins["blob"].([]byte)

	c := &config.RemoteConfig{}
	return c, json.Unmarshal(content, c)
}

func (s *ConfigStorage) Remotes() ([]*config.RemoteConfig, error) {
	stmnt := driver.NewStatement(s.ns, remotesSet)
	err := stmnt.Addfilter(driver.NewEqualFilter(urlField, s.url))
	if err != nil {
		return nil, err
	}

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
		return nil, err
	}

	var remotes []*config.RemoteConfig
	for r := range rs.Records {
		remote, err := remoteFromRecord(r)
		if err != nil {
			return nil, err
		}

		remotes = append(remotes, remote)
	}

	return remotes, nil
}

func (s *ConfigStorage) SetRemote(r *config.RemoteConfig) error {
	key, err := s.buildRemoteKey(r.Name)
	if err != nil {
		return err
	}

	json, err := json.Marshal(r)
	if err != nil {
		return err
	}

	bins := driver.BinMap{
		urlField: s.url,
		"name":   r.Name,
		"blob":   json,
	}

	return s.client.Put(nil, key, bins)
}

func (s *ConfigStorage) DeleteRemote(name string) error {
	key, err := s.buildRemoteKey(name)
	if err != nil {
		return err
	}

	_, err = s.client.Delete(nil, key)
	return err
}

func (s *ConfigStorage) buildRemoteKey(name string) (*driver.Key, error) {
	return driver.NewKey(s.ns, remotesSet, fmt.Sprintf("%s|%s", s.url, name))
}

func createIndexes(c *driver.Client, ns string) error {
	for _, set := range [...]string{
		referencesSet,
		remotesSet,
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
