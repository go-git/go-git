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

func FuzzFetchArgsDecode(f *testing.F) {
	f.Add([]byte("0032want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0009done\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		fa := &FetchArgs{}
		_ = fa.Decode(bytes.NewReader(data))
	})
}

func FuzzFetchOutputDecode(f *testing.F) {
	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	const delim, flush = "0001", "0000"
	// Negotiation round (acknowledgments flush-pkt, no packfile).
	f.Add([]byte(pkt("acknowledgments") + pkt("NAK") + flush))
	// Final round (acknowledgments delim-pkt ... packfile flush-pkt).
	f.Add([]byte(pkt("acknowledgments") + pkt("ready") + delim + pkt("packfile") + flush))
	// shallow-info section before the packfile.
	f.Add([]byte(pkt("shallow-info") + pkt("shallow "+oid) + delim + pkt("packfile") + flush))
	// wanted-refs section before the packfile.
	f.Add([]byte(pkt("wanted-refs") + pkt(oid+" refs/heads/main") + delim + pkt("packfile") + flush))
	// packfile-uris section before the packfile.
	f.Add([]byte(pkt("packfile-uris") + pkt(oid+" https://example/p.pack") + delim + pkt("packfile") + flush))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		fo := &FetchOutput{}
		_ = fo.Decode(bytes.NewReader(data))
	})
}

func FuzzCommandRequestDecode(f *testing.F) {
	const delim, flush = "0001", "0000"
	// command=ls-refs + one capability, delim, a ls-refs arg, flush.
	f.Add([]byte(pkt("command=ls-refs") + pkt("object-format=sha1") +
		delim + pkt("ref-prefix HEAD") + flush))
	f.Add([]byte(flush))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		cr := &CommandRequest{Args: &LsRefsArgs{}}
		_ = cr.Decode(bytes.NewReader(data))
	})
}

func FuzzCapabilityAdvDecode(f *testing.F) {
	const flush = "0000"
	f.Add([]byte(pkt("version 2") + pkt("ls-refs") + pkt("object-format=sha1") + flush))
	f.Add([]byte(pkt("version 2") + flush))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ca := &CapabilityAdv{}
		_ = ca.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsArgsDecode(f *testing.F) {
	const flush = "0000"
	f.Add([]byte(pkt("peel") + pkt("symrefs") + pkt("ref-prefix HEAD") + flush))
	f.Add([]byte(flush))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		la := &LsRefsArgs{}
		_ = la.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsOutputDecode(f *testing.F) {
	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	f.Add([]byte(pkt(oid+" refs/heads/main") + pkt(oid+" HEAD symref-target:refs/heads/main") + "0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		lo := &LsRefsOutput{}
		_ = lo.Decode(bytes.NewReader(data))
	})
}

func FuzzParseLsRefsLine(f *testing.F) {
	const oid = "6ecf0ef2c2dffb796033e5a02219af86ec6584e5"
	f.Add(oid + " refs/heads/main")
	f.Add(oid + " HEAD symref-target:refs/heads/main")
	f.Add(oid + " refs/tags/v1 peeled:" + oid)
	f.Add("")

	f.Fuzz(func(_ *testing.T, line string) {
		_, _ = parseLsRefsLine(line)
	})
}

// pkt frames s with a trailing LF as a single pkt-line, for fuzz seeds.
func pkt(s string) string {
	return fmt.Sprintf("%04x%s\n", len(s)+5, s)
}
