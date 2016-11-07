// +build ignore
package main

import (
	"C"
	"fmt"
	"math"
	"reflect"
	"sync"
)

type Handle uint64

const (
	ErrorCodeSuccess  = iota
	ErrorCodeNotFound = -iota
	ErrorCodeInternal = -iota
)

const MessageNotFound string = "object not found"
const InvalidHandle Handle = 0
const IH uint64 = uint64(InvalidHandle)

var counter Handle = InvalidHandle
var opMutex sync.Mutex
var registryHandle2Obj map[Handle]interface{} = map[Handle]interface{}{}
var registryObj2Handle map[uintptr][]Handle = map[uintptr][]Handle{}
var trace bool = false

func getNewHandle() Handle {
	counter++
	if counter == math.MaxUint64 {
		panic("Handle cache is exhausted")
	}
	return counter
}

func RegisterObject(obj interface{}) Handle {
	data_ptr := reflect.ValueOf(&obj).Elem().InterfaceData()[1]
	if trace {
		fmt.Printf("RegisterObject 0x%x\t%v\n", data_ptr, obj)
	}
	opMutex.Lock()
	defer opMutex.Unlock()
	handles, ok := registryObj2Handle[data_ptr]
	if ok {
		for _, h := range handles {
			other, ok := registryHandle2Obj[h]
			if !ok {
				panic("Inconsistent internal object mapping state (1)")
			}
			if other == obj {
				if trace {
					fmt.Printf("RegisterObject 0x%x reused %d\n", data_ptr, h)
				}
				return h
			}
		}
	}
	handle := getNewHandle()
	registryHandle2Obj[handle] = obj
	registryObj2Handle[data_ptr] = append(registryObj2Handle[data_ptr], handle)
	if trace {
		c_dump_objects()
	}
	return handle
}

func UnregisterObject(handle Handle) int {
	if trace {
		fmt.Printf("UnregisterObject %d\n", handle)
	}
	if handle == InvalidHandle {
		return ErrorCodeNotFound
	}
	opMutex.Lock()
	defer opMutex.Unlock()
	obj, ok := registryHandle2Obj[handle]
	if !ok {
		return ErrorCodeNotFound
	}
	delete(registryHandle2Obj, handle)
	data_ptr := reflect.ValueOf(&obj).Elem().InterfaceData()[1]
	other_handles, ok := registryObj2Handle[data_ptr]
	if !ok {
		panic(fmt.Sprintf("Inconsistent internal object mapping state (2): %d",
			handle))
	}
	hi := -1
	for i, h := range other_handles {
		if h == handle {
			hi = i
			break
		}
	}
	if hi < 0 {
		panic(fmt.Sprintf("Inconsistent internal object mapping state (3): %d",
			handle))
	}
	if len(other_handles) == 1 {
		delete(registryObj2Handle, data_ptr)
	} else {
		registryObj2Handle[data_ptr] = append(other_handles[:hi], other_handles[hi+1:]...)
	}
	if trace {
		c_dump_objects()
	}
	return ErrorCodeSuccess
}

func GetObject(handle Handle) (interface{}, bool) {
	if handle == InvalidHandle {
		return nil, false
	}
	opMutex.Lock()
	defer opMutex.Unlock()
	a, b := registryHandle2Obj[handle]
	return a, b
}

func GetHandle(obj interface{}) (Handle, bool) {
	data_ptr := reflect.ValueOf(&obj).Elem().InterfaceData()[1]
	opMutex.Lock()
	defer opMutex.Unlock()
	handles, ok := registryObj2Handle[data_ptr]
	if !ok {
		return InvalidHandle, false
	}
	for _, h := range handles {
		candidate := registryHandle2Obj[h]
		if candidate == obj {
			return h, true
		}
	}
	return InvalidHandle, false
}

func CopyString(str string) string {
	buf := make([]byte, len(str))
	copy(buf, []byte(str))
	return string(buf)
}

// https://github.com/golang/go/issues/14838
func CBytes(bytes []byte) *C.char {
	ptr := C.malloc(C.size_t(len(bytes)))
	copy((*[1 << 30]byte)(ptr)[:], bytes)
	return (*C.char)(ptr)
}

func SafeIsNil(v reflect.Value) bool {
	defer func() { recover() }()
	return v.IsNil()
}

//export c_dispose
func c_dispose(handle uint64) {
	UnregisterObject(Handle(handle))
}

//export c_objects_size
func c_objects_size() int {
	return len(registryHandle2Obj)
}

//export c_dump_objects
func c_dump_objects() {
	fmt.Printf("handles (%d):\n", len(registryHandle2Obj))
	for h, obj := range registryHandle2Obj {
		fmt.Printf("0x%x\t0x%x  %v\n", h,
			reflect.ValueOf(&obj).Elem().InterfaceData()[1], obj)
	}
	fmt.Println()
	phs := 0
	for _, h := range registryObj2Handle {
		phs += len(h)
	}
	fmt.Printf("pointers (%d):\n", phs)
	for ptr, h := range registryObj2Handle {
		fmt.Printf("0x%x\t%v\n", ptr, h)
	}
}

//export c_set_trace
func c_set_trace(val bool) {
	trace = val
}

// dummy main() is needed by the linker
func main() {}
