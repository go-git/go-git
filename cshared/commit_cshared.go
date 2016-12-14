// +build ignore
package main

import (
	"C"
	"io"
	"reflect"
	"unsafe"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"gopkg.in/src-d/go-git.v4/plumbing/storer"
)

//export c_Commit_get_Hash
func c_Commit_get_Hash(c uint64) *C.char {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return nil
	}
	commit := obj.(*object.Commit)
	return CBytes(commit.Hash[:])
}

//export c_Commit_get_Author
func c_Commit_get_Author(c uint64) uint64 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH
	}
	commit := obj.(*object.Commit)
	author := &commit.Author
	author_handle := RegisterObject(author)
	return uint64(author_handle)
}

//export c_Commit_get_Committer
func c_Commit_get_Committer(c uint64) uint64 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH
	}
	commit := obj.(*object.Commit)
	committer := &commit.Committer
	committer_handle := RegisterObject(committer)
	return uint64(committer_handle)
}

//export c_Commit_get_Message
func c_Commit_get_Message(c uint64) *C.char {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return nil
	}
	commit := obj.(*object.Commit)
	return C.CString(commit.Message)
}

//export c_Commit_Tree
func c_Commit_Tree(c uint64) uint64 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH
	}
	commit := obj.(*object.Commit)
	tree, err := commit.Tree()
	if err != nil {
		return IH
	}

	tree_handle := RegisterObject(tree)
	return uint64(tree_handle)
}

//export c_Commit_Parents
func c_Commit_Parents(c uint64) uint64 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH
	}
	commit := obj.(*object.Commit)
	parents := commit.Parents()
	parents_handle := RegisterObject(parents)
	return uint64(parents_handle)
}

//export c_Commit_NumParents
func c_Commit_NumParents(c uint64) int {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return -1
	}
	commit := obj.(*object.Commit)
	return commit.NumParents()
}

//export c_Commit_File
func c_Commit_File(c uint64, path string) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	commit := obj.(*object.Commit)
	file, err := commit.File(CopyString(path))
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	file_handle := RegisterObject(file)
	return uint64(file_handle), ErrorCodeSuccess, nil
}

//export c_Commit_ID
func c_Commit_ID(c uint64) *C.char {
	return c_Commit_get_Hash(c)
}

//export c_Commit_Type
func c_Commit_Type(c uint64) int8 {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return -1
	}
	commit := obj.(*object.Commit)
	return int8(commit.Type())
}

//export c_Commit_Decode
func c_Commit_Decode(o uint64) (uint64, int, *C.char) {
	commit := object.Commit{}
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	cobj := obj.(*plumbing.EncodedObject)
	err := commit.Decode(*cobj)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&commit)), ErrorCodeSuccess, nil
}

//export c_Commit_String
func c_Commit_String(c uint64) *C.char {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return nil
	}
	commit := obj.(*object.Commit)
	return C.CString(commit.String())
}

//export c_Commit_References
func c_Commit_References(c uint64, path string) (*C.char, int, int, *C.char) {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return nil, 0, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	commit := obj.(*object.Commit)
	refs, err := git.References(commit, CopyString(path))
	if err != nil {
		return nil, 0, ErrorCodeInternal, C.CString(err.Error())
	}
	handles := make([]uint64, len(refs))
	for i, c := range refs {
		handles[i] = uint64(RegisterObject(c))
	}
	size := 8 * len(handles)
	dest := C.malloc(C.size_t(size))
	header := (*reflect.SliceHeader)(unsafe.Pointer(&handles))
	header.Len *= 8
	copy((*[1 << 30]byte)(dest)[:], *(*[]byte)(unsafe.Pointer(header)))
	return (*C.char)(dest), size / 8, ErrorCodeSuccess, nil
}

//export c_Commit_Blame
func c_Commit_Blame(c uint64, path string) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(c))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	commit := obj.(*object.Commit)
	blame, err := git.Blame(commit, CopyString(path))
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(blame)), ErrorCodeSuccess, nil
}

//export c_NewCommitIter
func c_NewCommitIter(r uint64, iter uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	s := obj.(storer.EncodedObjectStorer)
	obj, ok = GetObject(Handle(iter))
	if !ok {
		return IH
	}
	obj_iter := obj.(storer.EncodedObjectIter)
	commit_iter := object.NewCommitIter(s, obj_iter)
	handle := RegisterObject(commit_iter)
	return uint64(handle)
}

//export c_CommitIter_Next
func c_CommitIter_Next(iter uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(iter))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	commitIter := obj.(*object.CommitIter)
	commit, err := commitIter.Next()
	if err != nil {
		if err == io.EOF {
			return IH, ErrorCodeSuccess, nil
		}
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	handle := RegisterObject(commit)
	return uint64(handle), ErrorCodeSuccess, nil
}
