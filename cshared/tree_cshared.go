// Created by cgo - DO NOT EDIT

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:2
package main

/*
//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:5

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:4
import (
//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:7
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:13

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:12
func c_Tree_get_Entries_len(t uint64) int {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return 0
	}
	tree := obj.(*object.Tree)
	return len(tree.Entries)
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:23

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:22
func c_Tree_get_Entries_item(t uint64, index int) (*_Ctype_char, uint32, *_Ctype_char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil, 0, nil
	}
	tree := obj.(*object.Tree)
	item := tree.Entries[index]
	return _Cfunc_CString(item.Name), uint32(item.Mode), CBytes(item.Hash[:])
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:34

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:33
func c_Tree_get_Hash(t uint64) *_Ctype_char {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tree := obj.(*object.Tree)
	return CBytes(tree.Hash[:])
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:44

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:43
func c_Tree_File(t uint64, path string) (uint64, int, *_Ctype_char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, _Cfunc_CString(MessageNotFound)
	}
	tree := obj.(*object.Tree)
	file, err := tree.File(CopyString(path))
	if err != nil {
		return IH, ErrorCodeInternal, _Cfunc_CString(err.Error())
	}
	return uint64(RegisterObject(file)), ErrorCodeSuccess, nil
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:58

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:57
func c_Tree_Type(t uint64) int8 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return -1
	}
	tree := obj.(*object.Tree)
	return int8(tree.Type())
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:68

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:67
func c_Tree_Files(t uint64) uint64 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH
	}
	tree := obj.(*object.Tree)
	iter := tree.Files()
	return uint64(RegisterObject(iter))
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:79

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:78
func c_Tree_Decode(o uint64) (uint64, int, *_Ctype_char) {
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH, ErrorCodeNotFound, _Cfunc_CString(MessageNotFound)
	}
	cobj := obj.(*plumbing.EncodedObject)
	tree := object.Tree{}
	err := tree.Decode(*cobj)
	if err != nil {
		return IH, ErrorCodeInternal, _Cfunc_CString(err.Error())
	}
	return uint64(RegisterObject(&tree)), ErrorCodeSuccess, nil
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:94

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:93
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
	tree := obj.(*object.Tree)
	walker := object.NewTreeIter(repo, tree)
	return uint64(RegisterObject(walker))
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:110

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:109
func c_TreeWalker_Next(tw uint64) (*_Ctype_char, *_Ctype_char, uint32, *_Ctype_char, int, *_Ctype_char) {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return nil, nil, 0, nil, ErrorCodeNotFound, _Cfunc_CString(MessageNotFound)
	}
	walker := obj.(*object.TreeIter)
	name, entry, err := walker.Next()
	if err != nil {
		return nil, nil, 0, nil, ErrorCodeInternal, _Cfunc_CString(err.Error())
	}
	return _Cfunc_CString(name), _Cfunc_CString(entry.Name), uint32(entry.Mode),
		CBytes(entry.Hash[:]), ErrorCodeSuccess, nil
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:125

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:124
func c_TreeWalker_Tree(tw uint64) uint64 {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return IH
	}
	walker := obj.(*object.TreeIter)
	return uint64(RegisterObject(walker.Tree()))
}

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:135

//line /home/mcuadros/workspace/go/src/gopkg.in/src-d/go-git.v4/cshared/tree_cshared.go:134
func c_TreeWalker_Close(tw uint64) {
	obj, ok := GetObject(Handle(tw))
	if !ok {
		return
	}
	walker := obj.(*object.TreeIter)
	walker.Close()
}
*/
