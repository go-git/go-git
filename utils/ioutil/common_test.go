package ioutil

import (
	"io"
	"strings"
)

func ExampleCheckClose() {
	// CheckClose is commonly used with named return values
	f := func() (err error) {
		// Get a io.ReadCloser
		r := io.NopCloser(strings.NewReader("foo"))

		// defer CheckClose call with an io.Closer and pointer to error
		defer CheckClose(r, &err)

		// ... work with r ...

		// if err is not nil, CheckClose will assign any close errors to it
		return err
	}

	err := f()
	if err != nil {
		panic(err)
	}
}
