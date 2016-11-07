// +build ignore
package main

import (
	"C"
	"reflect"
	"strings"
)

//export c_std_map_get_str_str
func c_std_map_get_str_str(m uint64, key string) *C.char {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return nil
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	}
	if mapval.Kind() != reflect.Map {
		return nil
	}
	val := mapval.MapIndex(reflect.ValueOf(key))
	if !val.IsValid() || SafeIsNil(val) {
		return nil
	}
	if (val.Kind() == reflect.Slice || val.Kind() == reflect.Array) &&
		val.Type().Elem().Kind() == reflect.Uint8 {
		arr := make([]byte, val.Len(), val.Len())
		reflect.Copy(reflect.ValueOf(arr), val)
		return CBytes(arr)
	}
	return C.CString(val.String())
}

//export c_std_map_get_str_obj
func c_std_map_get_str_obj(m uint64, key string) uint64 {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return IH
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	}
	if mapval.Kind() != reflect.Map {
		return IH
	}
	val := mapval.MapIndex(reflect.ValueOf(key))
	if !val.IsValid() || SafeIsNil(val) {
		return IH
	}
	val_handle := RegisterObject(val.Interface())
	return uint64(val_handle)
}

//export c_std_map_get_obj_obj
func c_std_map_get_obj_obj(m uint64, key uint64) uint64 {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return IH
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	}
	if mapval.Kind() != reflect.Map {
		return IH
	}
	obj, ok = GetObject(Handle(key))
	if !ok {
		return IH
	}
	val := mapval.MapIndex(reflect.ValueOf(obj))
	if !val.IsValid() || SafeIsNil(val) {
		return IH
	}
	val_handle := RegisterObject(val.Interface())
	return uint64(val_handle)
}

//export c_std_map_keys_str
func c_std_map_keys_str(m uint64) *C.char {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return nil
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	}
	if mapval.Kind() != reflect.Map {
		return nil
	}
	keys := mapval.MapKeys()
	keys_str := make([]string, 0, len(keys))
	for _, k := range keys {
		keys_str = append(keys_str, k.String())
	}
	return C.CString(strings.Join(keys_str, "\xff"))
}

//export c_std_map_len
func c_std_map_len(m uint64) int {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return -1
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	}
	if mapval.Kind() != reflect.Map {
		return -1
	}
	return mapval.Len()
}

//export c_std_map_set_str
func c_std_map_set_str(m uint64, key string, val uint64) {
	obj, ok := GetObject(Handle(m))
	if !ok {
		return
	}
	mapval := reflect.ValueOf(obj)
	if mapval.Kind() == reflect.Ptr {
		mapval = mapval.Elem()
	} else {
		return
	}
	if mapval.Kind() != reflect.Map {
		return
	}
	if val == IH {
		mapval.SetMapIndex(reflect.ValueOf(key), reflect.Value{})
	} else {
		obj, ok := GetObject(Handle(val))
		if !ok {
			return
		}
		mapval.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(obj))
	}
}
