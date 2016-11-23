// +build ignore
package main

import (
	"C"
	"strings"

	"golang.org/x/crypto/ssh"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/http"
	gssh "gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
)

//export c_NewBasicAuth
func c_NewBasicAuth(username, password string) uint64 {
	auth := http.NewBasicAuth(CopyString(username), CopyString(password))
	return uint64(RegisterObject(auth))
}

//export c_ParseRawPrivateKey
func c_ParseRawPrivateKey(pemBytes []byte) (uint64, int, *C.char) {
	pkey, err := ssh.ParseRawPrivateKey(pemBytes)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	// pointer is received - no need for &
	return uint64(RegisterObject(pkey)), ErrorCodeSuccess, nil
}

//export c_ParsePrivateKey
func c_ParsePrivateKey(pemBytes []byte) (uint64, int, *C.char) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&signer)), ErrorCodeSuccess, nil
}

//export c_NewPublicKey
func c_NewPublicKey(key uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(key))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	key_obj := obj.(ssh.PublicKey)
	pkey, err := ssh.NewPublicKey(key_obj)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&pkey)), ErrorCodeSuccess, nil
}

//export c_NewSignerFromKey
func c_NewSignerFromKey(key uint64) (uint64, int, *C.char) {
	obj, ok := GetObject(Handle(key))
	if !ok {
		return IH, ErrorCodeNotFound, C.CString(MessageNotFound)
	}
	signer, err := ssh.NewSignerFromKey(obj)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&signer)), ErrorCodeSuccess, nil
}

//export c_MarshalAuthorizedKey
func c_MarshalAuthorizedKey(key uint64) (*C.char, int) {
	obj, ok := GetObject(Handle(key))
	if !ok {
		return nil, 0
	}
	obj_key := obj.(ssh.PublicKey)
	mak := ssh.MarshalAuthorizedKey(obj_key)
	return C.CString(string(mak)), len(mak)
}

//export c_ParsePublicKey
func c_ParsePublicKey(in []byte) (uint64, int, *C.char) {
	pkey, err := ssh.ParsePublicKey(in)
	if err != nil {
		return IH, ErrorCodeInternal, C.CString(err.Error())
	}
	return uint64(RegisterObject(&pkey)), ErrorCodeSuccess, nil
}

//export c_ParseAuthorizedKey
func c_ParseAuthorizedKey(in []byte) (uint64, *C.char, *C.char, *C.char, int, int, *C.char) {
	pkey, comment, options, rest, err := ssh.ParseAuthorizedKey(in)
	if err != nil {
		return IH, nil, nil, nil, 0, ErrorCodeInternal,
			C.CString(err.Error())
	}
	pkey_handle := RegisterObject(&pkey)
	mopt := strings.Join(options, "\xff")
	return uint64(pkey_handle), C.CString(comment), C.CString(mopt),
		C.CString(string(rest)), len(rest), ErrorCodeSuccess, nil
}

//export c_ssh_Password_New
func c_ssh_Password_New(user, pass string) uint64 {
	obj := gssh.Password{User: CopyString(user), Pass: CopyString(pass)}
	return uint64(RegisterObject(&obj))
}

//export c_ssh_Password_get_User
func c_ssh_Password_get_User(p uint64) *C.char {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return nil
	}
	return C.CString(obj.(*gssh.Password).User)
}

//export c_ssh_Password_set_User
func c_ssh_Password_set_User(p uint64, v string) {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return
	}
	obj.(*gssh.Password).User = CopyString(v)
}

//export c_ssh_Password_get_Pass
func c_ssh_Password_get_Pass(p uint64) *C.char {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return nil
	}
	return C.CString(obj.(*gssh.Password).Pass)
}

//export c_ssh_Password_set_Pass
func c_ssh_Password_set_Pass(p uint64, v string) {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return
	}
	obj.(*gssh.Password).Pass = CopyString(v)
}

//c_ssh_PublicKeys_New
func c_ssh_PublicKeys_New(user string, signer uint64) uint64 {
	obj, ok := GetObject(Handle(signer))
	if !ok {
		return IH
	}
	pk := gssh.PublicKeys{User: CopyString(user), Signer: obj.(ssh.Signer)}
	return uint64(RegisterObject(&pk))
}

//export c_ssh_PublicKeys_get_User
func c_ssh_PublicKeys_get_User(p uint64) *C.char {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return nil
	}
	return C.CString(obj.(*gssh.PublicKeys).User)
}

//export c_ssh_PublicKeys_set_User
func c_ssh_PublicKeys_set_User(p uint64, v string) {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return
	}
	obj.(*gssh.PublicKeys).User = CopyString(v)
}

//export c_ssh_PublicKeys_get_Signer
func c_ssh_PublicKeys_get_Signer(p uint64) uint64 {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return IH
	}
	handle, ok := GetHandle(&obj.(*gssh.PublicKeys).Signer)
	if !ok {
		return IH
	}
	return uint64(handle)
}

//export c_ssh_PublicKeys_set_Signer
func c_ssh_PublicKeys_set_Signer(p uint64, v uint64) {
	obj, ok := GetObject(Handle(p))
	if !ok {
		return
	}
	signer, ok := GetObject(Handle(v))
	if !ok {
		return
	}
	obj.(*gssh.PublicKeys).Signer = *signer.(*ssh.Signer)
}
