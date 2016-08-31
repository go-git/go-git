package main

import (
	"encoding/json"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"

	"github.com/aerospike/aerospike-client-go"
)

const (
	urlField      = "url"
	referencesSet = "reference"
	remotesSet    = "remote"
)

type AerospikeStorage struct {
	client *aerospike.Client
	ns     string
	url    string

	os *AerospikeObjectStorage
	rs *AerospikeReferenceStorage
	cs *AerospikeConfigStorage
}

func NewAerospikeStorage(client *aerospike.Client, ns, url string) (*AerospikeStorage, error) {
	if err := createIndexes(client, ns); err != nil {
		return nil, err
	}

	return &AerospikeStorage{client: client, ns: ns, url: url}, nil
}

func (s *AerospikeStorage) ObjectStorage() core.ObjectStorage {
	if s.os == nil {
		s.os = &AerospikeObjectStorage{s.client, s.ns, s.url}
	}

	return s.os
}

func (s *AerospikeStorage) ReferenceStorage() core.ReferenceStorage {
	if s.rs == nil {
		s.rs = &AerospikeReferenceStorage{s.client, s.ns, s.url}
	}

	return s.rs
}

func (s *AerospikeStorage) ConfigStorage() config.ConfigStorage {
	if s.cs == nil {
		s.cs = &AerospikeConfigStorage{s.client, s.ns, s.url}
	}

	return s.cs
}

type AerospikeObjectStorage struct {
	client *aerospike.Client
	ns     string
	url    string
}

func (s *AerospikeObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

func (s *AerospikeObjectStorage) Set(obj core.Object) (core.Hash, error) {
	key, err := aerospike.NewKey(s.ns, obj.Type().String(), obj.Hash().String())
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

	bins := aerospike.BinMap{
		urlField: s.url,
		"hash":   obj.Hash().String(),
		"type":   obj.Type().String(),
		"blob":   c,
	}

	err = s.client.Put(nil, key, bins)
	return obj.Hash(), err
}

func (s *AerospikeObjectStorage) Get(t core.ObjectType, h core.Hash) (core.Object, error) {
	key, err := s.keyFromObject(h, t)
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

func (s *AerospikeObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	stmnt := aerospike.NewStatement(s.ns, t.String())
	err := stmnt.Addfilter(aerospike.NewEqualFilter(urlField, s.url))

	rs, err := s.client.Query(nil, stmnt)
	if err != nil {
		return nil, err
	}

	return &AerospikeObjectIter{t, rs.Records}, nil
}

func (s *AerospikeObjectStorage) keyFromObject(h core.Hash, t core.ObjectType,
) (*aerospike.Key, error) {
	return aerospike.NewKey(s.ns, t.String(), h.String())
}

type AerospikeObjectIter struct {
	t  core.ObjectType
	ch chan *aerospike.Record
}

func (i *AerospikeObjectIter) Next() (core.Object, error) {
	r := <-i.ch
	if r == nil {
		return nil, io.EOF
	}

	return objectFromRecord(r, i.t)
}

func (i *AerospikeObjectIter) ForEach(cb func(obj core.Object) error) error {
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

func (i *AerospikeObjectIter) Close() {}

func objectFromRecord(r *aerospike.Record, t core.ObjectType) (core.Object, error) {
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

type AerospikeReferenceStorage struct {
	client *aerospike.Client
	ns     string
	url    string
}

// Set stores a reference.
func (s *AerospikeReferenceStorage) Set(ref *core.Reference) error {
	key, err := aerospike.NewKey(s.ns, referencesSet, ref.Name().String())
	if err != nil {
		return err
	}

	raw := ref.Strings()
	bins := aerospike.BinMap{
		urlField: s.url,
		"name":   raw[0],
		"target": raw[1],
	}

	return s.client.Put(nil, key, bins)
}

// Get returns a stored reference with the given name
func (s *AerospikeReferenceStorage) Get(n core.ReferenceName) (*core.Reference, error) {
	key, err := aerospike.NewKey(s.ns, referencesSet, n.String())
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

// Iter returns a core.ReferenceIter
func (s *AerospikeReferenceStorage) Iter() (core.ReferenceIter, error) {
	stmnt := aerospike.NewStatement(s.ns, referencesSet)
	err := stmnt.Addfilter(aerospike.NewEqualFilter(urlField, s.url))
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

type AerospikeConfigStorage struct {
	client *aerospike.Client
	ns     string
	url    string
}

func (s *AerospikeConfigStorage) Remote(name string) (*config.RemoteConfig, error) {
	key, err := aerospike.NewKey(s.ns, remotesSet, name)
	if err != nil {
		return nil, err
	}

	rec, err := s.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	return remoteFromRecord(rec)
}

func remoteFromRecord(r *aerospike.Record) (*config.RemoteConfig, error) {
	content := r.Bins["blob"].([]byte)

	c := &config.RemoteConfig{}
	return c, json.Unmarshal(content, c)
}

func (s *AerospikeConfigStorage) Remotes() ([]*config.RemoteConfig, error) {
	stmnt := aerospike.NewStatement(s.ns, remotesSet)
	err := stmnt.Addfilter(aerospike.NewEqualFilter(urlField, s.url))
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

func (s *AerospikeConfigStorage) SetRemote(r *config.RemoteConfig) error {
	key, err := aerospike.NewKey(s.ns, remotesSet, r.Name)
	if err != nil {
		return err
	}

	json, err := json.Marshal(r)
	if err != nil {
		return err
	}

	bins := aerospike.BinMap{
		urlField: s.url,
		"name":   r.Name,
		"blob":   json,
	}

	return s.client.Put(nil, key, bins)
}

func (s *AerospikeConfigStorage) DeleteRemote(name string) error {
	key, err := aerospike.NewKey(s.ns, remotesSet, name)
	if err != nil {
		return err
	}

	_, err = s.client.Delete(nil, key)
	return err
}

func createIndexes(c *aerospike.Client, ns string) error {
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

func createIndex(c *aerospike.Client, ns, set string) error {
	task, err := c.CreateIndex(nil, ns, set, set, urlField, aerospike.STRING)
	if err != nil {
		if err.Error() == "Index already exists" {
			return nil
		}

		return err
	}

	return <-task.OnComplete()
}
