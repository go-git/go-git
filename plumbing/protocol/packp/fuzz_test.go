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
	// Multi-line deepen-not section: gives the fuzzer a starting point to grow
	// the section and exercise its maxSectionLines bound.
	f.Add([]byte("0032want 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0038deepen-not 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0038deepen-not 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0038deepen-not 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		fa := &FetchArgs{}
		_ = fa.Decode(bytes.NewReader(data))
	})
}

func FuzzFetchOutputDecode(f *testing.F) {
	// Seeds are inline pkt-line literals (no helper) so the OSS-Fuzz
	// go-118-fuzz-build harness, which lifts the Fuzz function out of this file,
	// compiles them standalone.
	//
	// Negotiation round (acknowledgments flush-pkt, no packfile).
	f.Add([]byte("0014acknowledgments\n0008NAK\n0000"))
	// Final round (acknowledgments delim-pkt ... packfile flush-pkt).
	f.Add([]byte("0014acknowledgments\n000aready\n0001000dpackfile\n0000"))
	// shallow-info section before the packfile.
	f.Add([]byte("0011shallow-info\n0035shallow 6ecf0ef2c2dffb796033e5a02219af86ec6584e5\n0001000dpackfile\n0000"))
	// wanted-refs section before the packfile.
	f.Add([]byte("0010wanted-refs\n003d6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n0001000dpackfile\n0000"))
	// packfile-uris section before the packfile.
	f.Add([]byte("0012packfile-uris\n00446ecf0ef2c2dffb796033e5a02219af86ec6584e5 https://example/p.pack\n0001000dpackfile\n0000"))
	// Multi-line packfile-uris section: gives the fuzzer a starting point to
	// grow the section and exercise its maxSectionLines bound.
	f.Add([]byte("0012packfile-uris\n00446ecf0ef2c2dffb796033e5a02219af86ec6584e5 https://example/p.pack\n00446ecf0ef2c2dffb796033e5a02219af86ec6584e5 https://example/p.pack\n00446ecf0ef2c2dffb796033e5a02219af86ec6584e5 https://example/p.pack\n0001000dpackfile\n0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		fo := &FetchOutput{}
		_ = fo.Decode(bytes.NewReader(data))
	})
}

func FuzzCommandRequestDecode(f *testing.F) {
	// command=ls-refs + one capability, delim, a ls-refs arg, flush.
	f.Add([]byte("0014command=ls-refs\n0017object-format=sha1\n00010014ref-prefix HEAD\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		cr := &CommandRequest{Args: &LsRefsArgs{}}
		_ = cr.Decode(bytes.NewReader(data))
	})
}

func FuzzCapabilityAdvDecode(f *testing.F) {
	f.Add([]byte("000eversion 2\n000cls-refs\n0017object-format=sha1\n0000"))
	f.Add([]byte("000eversion 2\n0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		ca := &CapabilityAdv{}
		_ = ca.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsArgsDecode(f *testing.F) {
	f.Add([]byte("0009peel\n000csymrefs\n0014ref-prefix HEAD\n0000"))
	f.Add([]byte("0000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		la := &LsRefsArgs{}
		_ = la.Decode(bytes.NewReader(data))
	})
}

func FuzzLsRefsOutputDecode(f *testing.F) {
	f.Add([]byte("003d6ecf0ef2c2dffb796033e5a02219af86ec6584e5 refs/heads/main\n00506ecf0ef2c2dffb796033e5a02219af86ec6584e5 HEAD symref-target:refs/heads/main\n0000"))
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
