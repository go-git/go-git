package main

import (
	"C"
	"io"
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

//export c_File_get_Name
func c_File_get_Name(f uint64) *C.char {
	obj, ok := GetObject(Handle(f))
	if !ok {
		return nil
	}
	file := obj.(*object.File)
	return C.CString(file.Name)
}

//export c_File_get_Mode
func c_File_get_Mode(f uint64) uint32 {
	obj, ok := GetObject(Handle(f))
	if !ok {
		return 0
	}
	file := obj.(*object.File)
	return uint32(file.Mode)
}

//export c_File_get_Hash
func c_File_get_Hash(b uint64) *C.char {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return nil
	}
	file := obj.(*object.File)
	return CBytes(file.Hash[:])
}

//export c_File_Size
func c_File_Size(b uint64) int64 {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return -1
	}
	file := obj.(*object.File)
	return file.Size
}

//export c_File_Decode
func c_File_Decode(o uint64) uint64 {
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH
	}
	cobj := obj.(*plumbing.EncodedObject)
	file := object.File{}
	file.Decode(*cobj)
	return uint64(RegisterObject(&file))
}

//export c_File_Read
func c_File_Read(b uint64) (int, *C.char) {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	file := obj.(*object.File)
	reader, err := file.Reader()
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

//export c_File_Type
func c_File_Type(c uint64) int8 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return -1
	}
	file := obj.(*object.File)
	return int8(file.Type())
}

//export c_NewFileIter
func c_NewFileIter(r uint64, t uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	storer := obj.(storer.EncodedObjectStorer)
	obj, ok = GetObject(Handle(t))
	if !ok {
		return IH
	}
	tree := obj.(*object.Tree)
	iter := object.NewFileIter(storer, tree)
	return uint64(RegisterObject(iter))
}

//export c_FileIter_Next
func c_FileIter_Next(i uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(i))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	iter := obj.(*object.FileIter)
	file, err := iter.Next()
	if err != nil {
		if err == io.EOF {
			return IH, ErrorCodeSuccess, nil
		}
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(file)), ErrorCodeSuccess, nil
}

//export c_FileIter_Close
func c_FileIter_Close(i uint64) {
	obj, ok := GetObject(Handle(i))
	if !ok {
		return
	}
	iter := obj.(*object.FileIter)
	iter.Close()
}
