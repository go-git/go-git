// +build ignore
package main

import (
	"C"

	"gopkg.in/src-d/go-git.v3"
	"gopkg.in/src-d/go-git.v3/core"
)

//export c_Tree_get_Entries_len
func c_Tree_get_Entries_len(t uint64) int {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return 0
	}
	tree := obj.(*git.Tree)
	return len(tree.Entries)
}

//export c_Tree_get_Entries_item
func c_Tree_get_Entries_item(t uint64, index int) (*C.char, uint32, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil, 0, nil
	}
	tree := obj.(*git.Tree)
	item := tree.Entries[index]
	return C.CString(item.Name), uint32(item.Mode), CBytes(item.Hash[:])
}

//export c_Tree_get_Hash
func c_Tree_get_Hash(t uint64) *C.char {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tree := obj.(*git.Tree)
	return CBytes(tree.Hash[:])
}

//export c_Tree_File
func c_Tree_File(t uint64, path string) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tree := obj.(*git.Tree)
	file, err := tree.File(CopyString(path))
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(file)), ErrorCodeSuccess, nil
}

//export c_Tree_Type
func c_Tree_Type(t uint64) int8 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return -1
	}
	tree := obj.(*git.Tree)
	return int8(tree.Type())
}

//export c_Tree_Files
func c_Tree_Files(t uint64) uint64 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH
	}
	tree := obj.(*git.Tree)
	iter := tree.Files()
	return uint64(RegisterObject(iter))
}

//export c_Tree_Decode
func c_Tree_Decode(o uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	cobj := obj.(*core.Object)
	tree := git.Tree{}
	err := tree.Decode(*cobj)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&tree)), ErrorCodeSuccess, nil
}

//export c_NewTreeWalker
func c_NewTreeWalker(r uint64, t uint64) uint64 {
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
	walker := git.NewTreeWalker(repo, tree)
	return uint64(RegisterObject(walker))
}

//export c_TreeWalker_Next
func c_TreeWalker_Next(tw uint64) (*C.char, *C.char, uint32, *C.char,
                                   uint64, int, *C.char) {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return nil, nil, 0, nil, IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	walker := obj.(*git.TreeWalker)
	name, entry, object, err := walker.Next()
	if err != nil {
		return nil, nil, 0, nil, IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return C.CString(name), C.CString(entry.Name), uint32(entry.Mode),
	       CBytes(entry.Hash[:]), uint64(RegisterObject(&object)),
	       ErrorCodeSuccess, nil
}

//export c_TreeWalker_Tree
func c_TreeWalker_Tree(tw uint64) uint64 {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return IH
	}
	walker := obj.(*git.TreeWalker)
	return uint64(RegisterObject(walker.Tree()))
}

//export c_TreeWalker_Close
func c_TreeWalker_Close(tw uint64) {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return
	}
	walker := obj.(*git.TreeWalker)
	walker.Close()
}