cshared
=======

Building
--------
go 1.6+
```
go build -o libgogit.so -buildmode=c-shared github.com/src-d/go-git/cshared
```
Two files must appear: libgogit.h and libgogit.so. The second must be
a shared library, not an ar archive (may happen when something goes wrong).
Check the exported symbols with `nm -g`.

How it works
------------

Nearly every public Go function is mirrored in the corresponding *_cshared.go
file. struct fields are also mirrored with getters and setters. The functions
are marked with `//export ...` "magic" cgo comment so that they appear
in defined symbols of a shared library built with `-buildmode=c-shared`.

Go pointers may not be passed out of cgo functions, so we maintain the
two-way registry of all active Go objects mapped to `Handle`-s (`uint64`).
Every time we need to return a reference to Go object outside, we call
`RegisterObject(interface{})` which returns a new `Handle` or reuses
an existing one if the object has already been registered. Then we
return the obtained `Handle`. When we need to receive a Go object reference
in cgo function parameters, we accept `uint64` and retrieve the `interface{}`
with `GetObject(Handle)` which can be casted to the underlying type with a
type assertion. When the object is no longer needed, we invoke
`UnregisterObject(Handle)`.

Although `interface{]` is just two `uintptr`-s inside, it is not a hashable
type and we cannot use it a as key in our backward registry mapping.
We are using the data `uintptr` as the key there. Since several distinct
objects may exist with the same data pointer (e.g. struct and first field
of the struct), the value of that mapping is a slice of `Handle`-s.

All the mentioned service functions are goroutine- and threadsafe.

`std_cshared.go` contains the cgo wrappers for standard library objects.

Debugging
---------
`c_dump_object()` prints the current state of the two-way object registry
to stdout. `c_set_trace()` activates echoing of `RegisterObject()` and
`UnregisterObject()` invocations.

Caveats
-------
Normally, we pass over a pointer to object as `interface{}` into `RegisterObject()`
so that it can be mutated later. It requires the corresponding
pointer-to-type type assertion in cgo functions. If you mess with this,
the cgo function will, of course, panic.

A cgo function is allowed to take Go's `string` parameters. `string`'s
data must point to some memory and cgo does not copy the incoming foreign
memory into Go memory automatically. What's worse, `string`-s are immutable
and when you copy it, the copy points to the same memory. This means that
if you pass in a `string` which was constructed using `malloc()`, for example,
and later `free()` it, all Go strings created from the function parameter
will point to the invalid memory. Actually, this allowance violates the
cgo pointer passing rules stated just several blocks of texts
below the example of string parameters - this is crazy, but we have to live
with this, as usual in Go world. So, *all incoming `string`-s must be immediately
safely copied with `CopyString()` once they are used*.

Returning strings and byte slices is also funny: you have to use `C.CString` -> `*C.char`
and additionally return the length as another result tuple member if needed.
`C.CString` copies the memory pointed by `string` to a `malloc()`-ed region
and it is the responsibility of the other side to `free()` it or it will leak
otherwise.

Another tricky part is in `c_std_map_get_str_str` and similar places
where you need to return `*C.char` from an unaddressable array accessed under
a pseudonym type through reflection. The only way I've found working
is using `reflect.Copy` to byte slice (copy), then `CBytes` (copy) and
finally another (copy) on the receiving side because the latter must be
`free()`-d.