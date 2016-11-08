// +build ignore
package main

import "C"

/*

//export c_Repository
func c_Repository() uint64 {
	repo := &git.Repository{}
	repo_handle := RegisterObject(repo)
	return uint64(repo_handle)
}

//export c_NewRepository
func c_NewRepository(url string, auth uint64) (uint64, int, *C.char) {
	var repo *git.Repository
	var err error
	url = CopyString(url)
	if auth != IH {
		real_auth, ok := GetObject(Handle(auth))
		if !ok {
			return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
		}
		repo, err = git.NewRepository(url, real_auth.(common.AuthMethod))
	} else {
		repo, err = git.NewRepository(url, nil)
	}
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	repo_handle := RegisterObject(repo)
	return uint64(repo_handle), ErrorCodeSuccess, nil
}

//export c_NewPlainRepository
func c_NewPlainRepository() uint64 {
	return uint64(RegisterObject(git.NewPlainRepository()))
}

//export c_Repository_get_Remotes
func c_Repository_get_Remotes(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	repo := obj.(*git.Repository)
	return uint64(RegisterObject(&repo.Remotes))
}

//export c_Repository_set_Remotes
func c_Repository_set_Remotes(r uint64, val uint64) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return
	}
	repo := obj.(*git.Repository)
	obj, ok = GetObject(Handle(val))
	if !ok {
		return
	}
	repo.Remotes = *obj.(*map[string]*git.Remote)
}

//export c_Repository_get_Storage
func c_Repository_get_Storage(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	repo := obj.(*git.Repository)
	return uint64(RegisterObject(&repo.Storage))
}

//export c_Repository_set_Storage
func c_Repository_set_Storage(r uint64, val uint64) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return
	}
	repo := obj.(*git.Repository)
	obj, ok = GetObject(Handle(val))
	if !ok {
		return
	}
	repo.Storage = *obj.(*plumbing.ObjectStorage)
}

//export c_Repository_Pull
func c_Repository_Pull(r uint64, remoteName, branch string) (int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	err := repo.Pull(remoteName, CopyString(branch))
	if err == nil {
		return ErrorCodeSuccess, nil
	}
	return ErrorCodeInternal, C.CString(err.Error())
}

//export c_Repository_PullDefault
func c_Repository_PullDefault(r uint64) (int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	err := repo.PullDefault()
	if err == nil {
		return ErrorCodeSuccess, nil
	}
	return ErrorCodeInternal, C.CString(err.Error())
}

//export c_Repository_Commit
func c_Repository_Commit(r uint64, h []byte) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	var hash plumbing.Hash
	copy(hash[:], h)
	commit, err := repo.Commit(hash)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	commit_handle := RegisterObject(commit)
	return uint64(commit_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Commits
func c_Repository_Commits(r uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	iter, err := repo.Commits()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	iter_handle := RegisterObject(iter)
	return uint64(iter_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Tree
func c_Repository_Tree(r uint64, h []byte) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	var hash plumbing.Hash
	copy(hash[:], h)
	tree, err := repo.Tree(hash)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	tree_handle := RegisterObject(tree)
	return uint64(tree_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Blob
func c_Repository_Blob(r uint64, h []byte) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	var hash plumbing.Hash
	copy(hash[:], h)
	blob, err := repo.Blob(hash)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	blob_handle := RegisterObject(blob)
	return uint64(blob_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Tag
func c_Repository_Tag(r uint64, h []byte) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	var hash plumbing.Hash
	copy(hash[:], h)
	tag, err := repo.Tag(hash)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	tag_handle := RegisterObject(tag)
	return uint64(tag_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Tags
func c_Repository_Tags(r uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	iter, err := repo.Tags()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	iter_handle := RegisterObject(iter)
	return uint64(iter_handle), ErrorCodeSuccess, nil
}

//export c_Repository_Object
func c_Repository_Object(r uint64, h []byte) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	repo := obj.(*git.Repository)
	var hash plumbing.Hash
	copy(hash[:], h)
	robj, err := repo.Object(hash)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	robj_handle := RegisterObject(robj)
	return uint64(robj_handle), ErrorCodeSuccess, nil
}

*/
