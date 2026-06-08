package packp

import (
	"bytes"
	"fmt"
	"testing"
)

func FuzzAdvRefsDecode(f *testing.F) {
	// Minimal well-formed advertisement: a single HEAD ref with one
	// capability followed by a flush packet. Lets the fuzzer mutate
	// from the success path rather than only the error path.
	f.Add([]byte("003b6ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD\x00ofs-delta0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ar := &AdvRefs{}
		_ = ar.Decode(bytes.NewReader(data))
	})
}

func FuzzUlReqDecode(f *testing.F) {
	f.Add([]byte("0032want 0000000000000000000000000000000000000000\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ur := &UploadRequest{}
		_ = ur.Decode(bytes.NewReader(data))
	})
}

func FuzzUpdReqDecode(f *testing.F) {
	// Minimal well-formed command-and-capabilities frame: a create
	// of refs/heads/main with report-status, followed by a flush.
	// Distinct non-zero hashes are required to pass validation.
	oldHash := "1ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	newHash := "2ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	payload := oldHash + " " + newHash + " refs/heads/main\x00report-status"
	frame := fmt.Sprintf("%04x%s0000", len(payload)+4, payload)

	f.Add([]byte(frame))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ur := &UpdateRequests{}
		_ = ur.Decode(bytes.NewReader(data))
	})
}

func FuzzServerResponseDecode(f *testing.F) {
	f.Add([]byte("0008NAK\n"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		sr := &ServerResponse{}
		_ = sr.Decode(bytes.NewReader(data))
	})
}

func FuzzShallowUpdateDecode(f *testing.F) {
	f.Add([]byte("0034shallow aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		su := &ShallowUpdate{}
		_ = su.Decode(bytes.NewReader(data))
	})
}

func FuzzReportStatusDecode(f *testing.F) {
	f.Add([]byte("000eunpack ok\n0019ok refs/heads/master\n0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		rs := &ReportStatus{}
		_ = rs.Decode(bytes.NewReader(data))
	})
}

func FuzzGitProtoDecode(f *testing.F) {
	f.Add([]byte("002ecommand pathname\x00host=host\x00\x00param1\x00param2\x00"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		gp := &GitProtoRequest{}
		_ = gp.Decode(bytes.NewReader(data))
	})
}

func FuzzPushOptionsDecode(f *testing.F) {
	f.Add([]byte("0015SomeKey=SomeValue0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		po := &PushOptions{}
		_ = po.Decode(bytes.NewReader(data))
	})
}
