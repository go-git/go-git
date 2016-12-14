// +build ignore
package main

import (
	"C"
	"io/ioutil"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

//export c_Signature_Name
func c_Signature_Name(s uint64) *C.char {
	obj, ok := GetObject(Handle(s))
	if !ok {
		return nil
	}
	sign := obj.(*object.Signature)
	return C.CString(sign.Name)
}

//export c_Signature_Email
func c_Signature_Email(s uint64) *C.char {
	obj, ok := GetObject(Handle(s))
	if !ok {
		return nil
	}
	sign := obj.(*object.Signature)
	return C.CString(sign.Email)
}

//export c_Signature_When
func c_Signature_When(s uint64) *C.char {
	obj, ok := GetObject(Handle(s))
	if !ok {
		return nil
	}
	sign := obj.(*object.Signature)
	return C.CString(sign.When.Format(time.RFC3339))
}

//export c_Signature_Decode
func c_Signature_Decode(b []byte) uint64 {
	sign := object.Signature{}
	sign.Decode(b)
	return uint64(RegisterObject(&sign))
}

//export c_Blob_get_Hash
func c_Blob_get_Hash(b uint64) *C.char {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return nil
	}
	blob := obj.(*object.Blob)
	return CBytes(blob.Hash[:])
}

//export c_Blob_Size
func c_Blob_Size(b uint64) int64 {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return -1
	}
	blob := obj.(*object.Blob)
	return blob.Size
}

//export c_Blob_Decode
func c_Blob_Decode(o uint64) uint64 {
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH
	}
	cobj := obj.(*plumbing.EncodedObject)
	blob := object.Blob{}
	blob.Decode(*cobj)
	return uint64(RegisterObject(&blob))
}

//export c_Blob_Read
func c_Blob_Read(b uint64) (int, *C.char) {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	blob := obj.(*object.Blob)
	reader, err := blob.Reader()
	if err != nil {
		return ErrorCodeInternal, C.CString(err.Error())
	}
	data, err := ioutil.ReadAll(reader)
	reader.Close()
	if err != nil {
		return ErrorCodeInternal, C.CString(err.Error())
	}
	return len(data), CBytes(data)
}

//export c_Blob_Type
func c_Blob_Type(c uint64) int8 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return -1
	}
	blob := obj.(*object.Blob)
	return int8(blob.Type())
}
