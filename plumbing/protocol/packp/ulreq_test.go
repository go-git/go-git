package packp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/format/pktline"
)

func ExampleUlReqEncoder_Encode() {
	// Create an empty UlReq with the contents you want...
	ur := NewUploadRequest()

	// Add a couple of wants
	ur.Wants = append(ur.Wants, plumbing.NewHash("3333333333333333333333333333333333333333"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("1111111111111111111111111111111111111111"))
	ur.Wants = append(ur.Wants, plumbing.NewHash("2222222222222222222222222222222222222222"))

	// And some capabilities you will like the server to use
	ur.Capabilities.Add("sysref", "HEAD:/refs/heads/master")
	ur.Capabilities.Add("ofs-delta")

	// Add a couple of shallows
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	ur.Shallows = append(ur.Shallows, plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	// And retrict the answer of the server to commits newer than "2015-01-02 03:04:05 UTC"
	since := time.Date(2015, time.January, 2, 3, 4, 5, 0, time.UTC)
	ur.Depth = DepthSince(since)

	// Create a new Encode for the stdout...
	e := newUlReqEncoder(os.Stdout)
	// ...and encode the upload-request to it.
	_ = e.Encode(ur) // ignoring errors for brevity
	// Output:
	// 005bwant 1111111111111111111111111111111111111111 ofs-delta sysref=HEAD:/refs/heads/master
	// 0032want 2222222222222222222222222222222222222222
	// 0032want 3333333333333333333333333333333333333333
	// 0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
	// 0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
	// 001cdeepen-since 1420167845
	// 0000
}

func ExampleUlReqDecoder_Decode() {
	// Here is a raw advertised-ref message.
	raw := "" +
		"005bwant 1111111111111111111111111111111111111111 ofs-delta sysref=HEAD:/refs/heads/master\n" +
		"0032want 2222222222222222222222222222222222222222\n" +
		"0032want 3333333333333333333333333333333333333333\n" +
		"0035shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"0035shallow bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"001cdeepen-since 1420167845\n" + // 2015-01-02 03:04:05 +0000 UTC
		pktline.FlushString

	// Use the raw message as our input.
	input := strings.NewReader(raw)

	// Create the Decoder reading from our input.
	d := newUlReqDecoder(input)

	// Decode the input into a newly allocated UlReq value.
	ur := NewUploadRequest()
	_ = d.Decode(ur) // error check ignored for brevity

	// Do something interesting with the UlReq, e.g. print its contents.
	fmt.Println("capabilities =", ur.Capabilities.String())
	fmt.Println("wants =", ur.Wants)
	fmt.Println("shallows =", ur.Shallows)
	switch depth := ur.Depth.(type) {
	case DepthCommits:
		fmt.Println("depth =", int(depth))
	case DepthSince:
		fmt.Println("depth =", time.Time(depth))
	case DepthReference:
		fmt.Println("depth =", string(depth))
	}
	// Output:
	// capabilities = ofs-delta sysref=HEAD:/refs/heads/master
	// wants = [1111111111111111111111111111111111111111 2222222222222222222222222222222222222222 3333333333333333333333333333333333333333]
	// shallows = [aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb]
	// depth = 2015-01-02 03:04:05 +0000 UTC
}
