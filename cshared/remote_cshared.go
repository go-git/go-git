// +build ignore
package main

import "C"

/*

//export c_Remote_get_Endpoint
func c_Remote_get_Endpoint(r uint64) *C.char {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return nil
	}
	remote := obj.(*git.Remote)
	return C.CString(string(remote.Endpoint))
}

//export c_Remote_set_Endpoint
func c_Remote_set_Endpoint(r uint64, value string) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return
	}
	remote := obj.(*git.Remote)
	remote.Endpoint = common.Endpoint(CopyString(value))
}

//export c_Remote_get_Auth
func c_Remote_get_Auth(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	remote := obj.(*git.Remote)
	return uint64(RegisterObject(&remote.Auth))
}

//export c_Remote_set_Auth
func c_Remote_set_Auth(r uint64, value uint64) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return
	}
	remote := obj.(*git.Remote)
	obj, ok = GetObject(Handle(value))
	if !ok {
		return
	}
	remote.Auth = *obj.(*common.AuthMethod)
}

//export c_NewRemote
func c_NewRemote(url string) (uint64, int, *C.char) {
	remote, err := git.NewRemote(CopyString(url))
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(remote)), ErrorCodeSuccess, nil
}

//export c_NewAuthenticatedRemote
func c_NewAuthenticatedRemote(url string, auth uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(auth))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	auth_method := *obj.(*common.AuthMethod)
	remote, err := git.NewAuthenticatedRemote(CopyString(url), auth_method)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(remote)), ErrorCodeSuccess, nil
}

//export c_Remote_Connect
func c_Remote_Connect(r uint64) (int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return ErrorCodeNotFound, nil
	}
	remote := obj.(*git.Remote)
	err := remote.Connect()
	if err != nil {
		return ErrorCodeInternal, C.CString(err.Error())
	}
	return ErrorCodeSuccess, nil
}

//export c_Remote_Info
func c_Remote_Info(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	remote := obj.(*git.Remote)
	return uint64(RegisterObject(remote.Info()))
}

//export c_Remote_Capabilities
func c_Remote_Capabilities(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	remote := obj.(*git.Remote)
	return uint64(RegisterObject(remote.Capabilities()))
}

//export c_Remote_DefaultBranch
func c_Remote_DefaultBranch(r uint64) *C.char {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return nil
	}
	remote := obj.(*git.Remote)
	return C.CString(remote.DefaultBranch())
}

//export c_Remote_Head
func c_Remote_Head(r uint64) (*C.char, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return nil, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	remote := obj.(*git.Remote)
	hash, err := remote.Head()
	if err != nil {
		return nil, ErrorCodeInternal, C.CString(err.Error())
	}
	return CBytes(hash[:]), ErrorCodeSuccess, nil
}

//export c_Remote_Fetch
func c_Remote_Fetch(r uint64, req uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	remote := obj.(*git.Remote)
	obj, ok = GetObject(Handle(req))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	request := obj.(*common.UploadPackRequest)
	reader, err := remote.Fetch(request)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(reader)), ErrorCodeSuccess, nil
}

//export c_Remote_FetchDefaultBranch
func c_Remote_FetchDefaultBranch(r uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	remote := obj.(*git.Remote)
	reader, err := remote.FetchDefaultBranch()
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(reader)), ErrorCodeSuccess, nil
}

//export c_Remote_Ref
func c_Remote_Ref(r uint64, refName string) (*C.char, int, *C.char) {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return nil, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	remote := obj.(*git.Remote)
	hash, err := remote.Ref(CopyString(refName))
	if err != nil {
		return nil, ErrorCodeInternal, C.CString(err.Error())
	}
	return CBytes(hash[:]), ErrorCodeSuccess, nil
}

//export c_Remote_Refs
func c_Remote_Refs(r uint64) uint64 {
	obj, ok := GetObject(Handle(r))
	if !ok {
		return IH
	}
	remote := obj.(*git.Remote)
	refs := remote.Refs()
	return uint64(RegisterObject(refs))
}

*/
