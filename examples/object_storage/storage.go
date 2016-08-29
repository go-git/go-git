package main

import (
	"fmt"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/config"
	"gopkg.in/src-d/go-git.v4/core"

	"github.com/aerospike/aerospike-client-go"
)

// CREATE INDEX commits ON test.commit (url) STRING;
// CREATE INDEX blobs ON test.blob (url) STRING;

type AerospikeStorage struct {
	url    string
	client *aerospike.Client
	os     *AerospikeObjectStorage
	rs     *AerospikeReferenceStorage
}

func NewAerospikeStorage(url string, client *aerospike.Client) *AerospikeStorage {
	return &AerospikeStorage{url: url, client: client}
}

func (s *AerospikeStorage) ObjectStorage() core.ObjectStorage {
	if s.os == nil {
		s.os = NewAerospikeObjectStorage(s.url, s.client)
	}

	return s.os
}

func (s *AerospikeStorage) ReferenceStorage() core.ReferenceStorage {
	if s.rs == nil {
		s.rs = NewAerospikeReferenceStorage(s.url, s.client)
	}

	return s.rs
}

func (s *AerospikeStorage) ConfigStorage() config.ConfigStorage {
	return &ConfigStorage{}
}

type AerospikeObjectStorage struct {
	url    string
	client *aerospike.Client
}

func NewAerospikeObjectStorage(url string, c *aerospike.Client) *AerospikeObjectStorage {
	return &AerospikeObjectStorage{url, c}
}

func (s *AerospikeObjectStorage) NewObject() core.Object {
	return &core.MemoryObject{}
}

func (o *AerospikeObjectStorage) Set(obj core.Object) (core.Hash, error) {
	key, err := aerospike.NewKey("test", obj.Type().String(), obj.Hash().String())
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
		"url":  o.url,
		"hash": obj.Hash().String(),
		"type": obj.Type().String(),
		"blob": c,
	}

	err = o.client.Put(nil, key, bins)
	fmt.Println(err, key)
	return obj.Hash(), err
}

func (o *AerospikeObjectStorage) Get(h core.Hash, t core.ObjectType) (core.Object, error) {
	key, err := keyFromObject(h, t)
	if err != nil {
		return nil, err
	}

	rec, err := o.client.Get(nil, key)
	if err != nil {
		return nil, err
	}

	fmt.Println(rec.Bins)
	return nil, core.ErrObjectNotFound
}

func (o *AerospikeObjectStorage) Iter(t core.ObjectType) (core.ObjectIter, error) {
	s := aerospike.NewStatement("test", t.String())
	err := s.Addfilter(aerospike.NewEqualFilter("url", o.url))

	rs, err := o.client.Query(nil, s)
	if err != nil {
		return nil, err
	}

	return &AerospikeObjectIter{t, rs.Records}, nil
}

func keyFromObject(h core.Hash, t core.ObjectType) (*aerospike.Key, error) {
	return aerospike.NewKey("test", t.String(), h.String())
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

	content := r.Bins["blob"].([]byte)

	o := &core.MemoryObject{}
	o.SetType(i.t)
	o.SetSize(int64(len(content)))

	_, err := o.Write(content)
	if err != nil {
		return nil, err
	}

	return o, nil
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

type AerospikeReferenceStorage struct {
	url    string
	client *aerospike.Client
}

func NewAerospikeReferenceStorage(url string, c *aerospike.Client) *AerospikeReferenceStorage {
	return &AerospikeReferenceStorage{url, c}
}

// Set stores a reference.
func (s *AerospikeReferenceStorage) Set(ref *core.Reference) error {
	key, err := aerospike.NewKey("test", "references", ref.Name().String())
	if err != nil {
		return err
	}

	raw := ref.Strings()
	bins := aerospike.BinMap{
		"url":    s.url,
		"name":   raw[0],
		"target": raw[1],
	}

	return s.client.Put(nil, key, bins)
}

// Get returns a stored reference with the given name
func (s *AerospikeReferenceStorage) Get(n core.ReferenceName) (*core.Reference, error) {
	key, err := aerospike.NewKey("test", "references", n.String())
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
	stmnt := aerospike.NewStatement("test", "references")
	err := stmnt.Addfilter(aerospike.NewEqualFilter("url", s.url))
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
	RemotesConfig map[string]*config.RemoteConfig
}

func (c *ConfigStorage) Remote(name string) (*config.RemoteConfig, error) {
	r, ok := c.RemotesConfig[name]
	if ok {
		return r, nil
	}

	return nil, config.ErrRemoteConfigNotFound
}

func (c *ConfigStorage) Remotes() ([]*config.RemoteConfig, error) {
	var o []*config.RemoteConfig
	for _, r := range c.RemotesConfig {
		o = append(o, r)
	}

	return o, nil
}
func (c *ConfigStorage) SetRemote(r *config.RemoteConfig) error {
	c.RemotesConfig[r.Name] = r
	return nil
}

func (c *ConfigStorage) DeleteRemote(name string) error {
	delete(c.RemotesConfig, name)
	return nil
}
