// +build ignore
package main

import (
	"C"

	"gopkg.in/src-d/go-git.v3"
)

//export c_Blame_get_Path
func c_Blame_get_Path(b uint64) *C.char {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return nil
	}
	blame := obj.(*git.Blame)
	return C.CString(blame.Path)
}

//export c_Blame_get_Rev
func c_Blame_get_Rev(b uint64) *C.char {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return nil
	}
	blame := obj.(*git.Blame)
	return CBytes(blame.Rev[:])
}


//export c_Blame_get_Lines_len
func c_Blame_get_Lines_len(b uint64) int {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return 0
	}
	blame := obj.(*git.Blame)
	return len(blame.Lines)
}

//export c_Blame_get_Lines_item
func c_Blame_get_Lines_item(b uint64, i int) {
	obj, ok := GetObject(Handle(b))
	if !ok {
		return
	}
	blame := obj.(*git.Blame)
	line := blame.Lines[i]
	_ = line
}

