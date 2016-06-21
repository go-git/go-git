package main

import (
	"C"

	"gopkg.in/src-d/go-git.v3"
)

//export c_File_get_Name
func c_File_get_Name(f uint64) *C.char {
	obj, ok := GetObject(Handle(f))
	if !ok {
		return nil
	}
	file := obj.(*git.File)
	return C.CString(file.Name)
}

//export c_File_get_Mode
func c_File_get_Mode(f uint64) uint32 {
	obj, ok := GetObject(Handle(f))
	if !ok {
		return 0
	}
	file := obj.(*git.File)
	return uint32(file.Mode)
}

//export c_NewFileIter
func c_NewFileIter(r uint64, t uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	repo := obj.(*git.Repository)
	obj, ok = GetObject(Handle(t))
	if !ok {
		return IH
	}
	tree := obj.(*git.Tree)
	iter := git.NewFileIter(repo, tree)
	return uint64(RegisterObject(iter))
}

//export c_FileIter_Next
func c_FileIter_Next(i uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(i))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	iter := obj.(*git.FileIter)
	file, err := iter.Next()
	if err != nil {
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
	iter := obj.(*git.FileIter)
	iter.Close()
}
