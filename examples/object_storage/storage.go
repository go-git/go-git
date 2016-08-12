package main

import (
	"fmt"
	"io"

	"gopkg.in/src-d/go-git.v3/core"
	"gopkg.in/src-d/go-git.v3/storage/memory"

	"github.com/aerospike/aerospike-client-go"
)

// CREATE INDEX commits ON test.commit (url) STRING;
// CREATE INDEX blobs ON test.blob (url) STRING;

type AerospikeObjectStorage struct {
	url    string
	client *aerospike.Client
}

func NewAerospikeObjectStorage(url string, c *aerospike.Client) *AerospikeObjectStorage {
	return &AerospikeObjectStorage{url, c}
}

func (o *AerospikeObjectStorage) Set(obj core.Object) (core.Hash, error) {
	key, err := aerospike.NewKey("test", obj.Type().String(), obj.Hash().String())
	if err != nil {
		return obj.Hash(), err
	}

	bins := aerospike.BinMap{
		"url":  o.url,
		"hash": obj.Hash().String(),
		"type": obj.Type().String(),
		"blob": obj.Content(),
	}

	err = o.client.Put(nil, key, bins)
	fmt.Println(err, key)
	return obj.Hash(), err
}

func (o *AerospikeObjectStorage) Get(h core.Hash) (core.Object, error) {
	key, err := keyFromObject(h)
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

func keyFromObject(h core.Hash) (*aerospike.Key, error) {
	return aerospike.NewKey("test", "objects", h.String())
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
	return memory.NewObject(i.t, int64(len(content)), content), nil
}

func (i *AerospikeObjectIter) Close() {}
