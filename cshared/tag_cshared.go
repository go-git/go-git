// +build ignore
package main

import (
	"C"
	"io"
	
	"gopkg.in/src-d/go-git.v3"
	"gopkg.in/src-d/go-git.v3/core"
)

func c_Tag_get_Hash(t uint64) *C.char {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tag := obj.(*git.Tag)
	return CBytes(tag.Hash[:])
}

func c_Tag_get_Name(t uint64) *C.char {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tag := obj.(*git.Tag)
	return C.CString(tag.Name)
}

func c_Tag_get_Tagger(t uint64) uint64 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH
	}
	tag := obj.(*git.Tag)
	return uint64(RegisterObject(&tag.Tagger))
}

func c_Tag_get_Message(t uint64) *C.char {
  obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tag := obj.(*git.Tag)
	return C.CString(tag.Message)
}

func c_Tag_get_TargetType(t uint64) int8 {
  obj, ok := GetObject(Handle(t))
	if !ok {
		return -1
	}
	tag := obj.(*git.Tag)
	return int8(tag.TargetType)
}

func c_Tag_get_Target(t uint64) *C.char {
  obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tag := obj.(*git.Tag)
	return CBytes(tag.Target[:])
}

//export c_Tag_Type
func c_Tag_Type(t uint64) int8 {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return -1
	}
	tag := obj.(*git.Tag)
	return int8(tag.Type())
}

//export c_Tag_Decode
func c_Tag_Decode(o uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(o))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	cobj := obj.(*core.Object)
	tag := git.Tag{}
	err := tag.Decode(*cobj)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&tag)), ErrorCodeSuccess, nil
}

//export c_Tag_Commit
func c_Tag_Commit(t uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tag := obj.(*git.Tag)
	commit, err := tag.Commit()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(commit)), ErrorCodeSuccess, nil
}

//export c_Tag_Tree
func c_Tag_Tree(t uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tag := obj.(*git.Tag)
	tree, err := tag.Tree()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(tree)), ErrorCodeSuccess, nil
}

//export c_Tag_Blob
func c_Tag_Blob(t uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tag := obj.(*git.Tag)
	blob, err := tag.Blob()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(blob)), ErrorCodeSuccess, nil
}

//export c_Tag_Object
func c_Tag_Object(t uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tag := obj.(*git.Tag)
	object, err := tag.Object()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&object)), ErrorCodeSuccess, nil
}

//export c_Tag_String
func c_Tag_String(t uint64) *C.char {
	obj, ok := GetObject(Handle(t))
	if !ok {
		return nil
	}
	tag := obj.(*git.Tag)
	return C.CString(tag.String())
}

//export c_NewTagIter
func c_NewTagIter(r uint64, i uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	repo := obj.(*git.Repository)
	obj, ok = GetObject(Handle(i))
	if !ok {
		return IH
	}
	iter := obj.(*core.ObjectIter)
	return uint64(RegisterObject(git.NewTagIter(repo, *iter)))
}

//export c_TagIter_Next
func c_TagIter_Next(i uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(i))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	tagiter := obj.(*git.TagIter)
	tag, err := tagiter.Next()
	if err != nil {
		if err == io.EOF {
			return IH, ErrorCodeSuccess, nil
		}
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(tag)), ErrorCodeSuccess, nil
}
