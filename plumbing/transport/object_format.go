package transport

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/pktline"
	"github.com/go-git/go-git/v6/plumbing/protocol/capability"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp/sideband"
	"github.com/go-git/go-git/v6/storage"
)

// objectFormat returns the repository's configured object format, defaulting to
// the package default when the storer is nil or its config is missing or unset.
// A typed nil storer, such as a nil *memory.Storage, counts as nil: SendPack
// ignored its storer before it checked the object format, and callers may
// still pass one.
func objectFormat(st storage.Storer) config.ObjectFormat {
	if st == nil {
		return config.DefaultObjectFormat
	}
	if v := reflect.ValueOf(st); v.Kind() == reflect.Pointer && v.IsNil() {
		return config.DefaultObjectFormat
	}
	cfg, err := st.Config()
	if err != nil || cfg == nil {
		return config.DefaultObjectFormat
	}
	if cfg.Extensions.ObjectFormat == config.UnsetObjectFormat {
		return config.DefaultObjectFormat
	}
	return cfg.Extensions.ObjectFormat
}

// sizeZeroIDs returns cmds with their zero old or new ids sized for f. Create
// and delete commands carry plumbing.ZeroHash, which encodes as 40 zeros;
// git's receive-pack parses ids at the repository's size and rejects a short
// zero id on a sha256 repository as a protocol error.
//
// cmds itself is returned when nothing needs resizing, which includes every
// sha1 request. Otherwise the result holds copies, and the request's own
// commands are not modified.
func sizeZeroIDs(cmds []*packp.Command, f config.ObjectFormat) []*packp.Command {
	hasZero := func(c *packp.Command) bool { return c.Old.IsZero() || c.New.IsZero() }
	if f == config.SHA1 || !slices.ContainsFunc(cmds, hasZero) {
		return cmds
	}

	copies := make([]packp.Command, len(cmds))
	out := make([]*packp.Command, len(cmds))
	for i, c := range cmds {
		copies[i] = *c
		if c.Old.IsZero() {
			copies[i].Old.ResetBySize(f.Size())
		}
		if c.New.IsZero() {
			copies[i].New.ResetBySize(f.Size())
		}
		out[i] = &copies[i]
	}
	return out
}

// receivePackObjectFormatError mirrors receive-pack.c read_head_info: the
// client's format must equal the repository's. A client that does not send
// the capability speaks sha1; one that sends it more than once is held to
// the first value, as parse_feature_value reads it, and a bare
// object-format counts as the empty format. The value is echoed through
// escapeClientValue.
func receivePackObjectFormatError(repo config.ObjectFormat, caps capability.List) *pktline.ErrorLine {
	client := config.SHA1.String()
	if caps.Supports(capability.ObjectFormat) {
		client = ""
		if vals := caps.Get(capability.ObjectFormat); len(vals) > 0 {
			client = vals[0]
		}
	}
	if client == repo.String() {
		return nil
	}
	return &pktline.ErrorLine{Text: fmt.Sprintf("unsupported object format '%s'", escapeClientValue(client))}
}

// escapeClientValue returns v with its control characters and invalid UTF-8
// replaced by '?', for echoing a client's value in an error. The vreportf
// behind git's die does the same for control characters other than tab and
// newline; those two are replaced here as well, so the value cannot add lines
// to the error.
func escapeClientValue(v string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return '?'
		}
		return r
	}, v)
}

// v2ObjectFormatError mirrors serve.c object_format_receive and
// process_request. A client that does not send the capability speaks sha1.
// serve.c checks each object-format line as it reads it and keeps the last
// one, so every value must be known and the last must equal the
// repository's format. An unknown value is echoed through escapeClientValue;
// a mismatched one is already known to be sha1 or sha256.
func v2ObjectFormatError(repo config.ObjectFormat, caps capability.List) *pktline.ErrorLine {
	client := config.SHA1.String()
	if caps.Supports(capability.ObjectFormat) {
		vals := caps.Get(capability.ObjectFormat)
		if len(vals) == 0 {
			return &pktline.ErrorLine{Text: "object-format capability requires an argument"}
		}
		for _, v := range vals {
			switch config.ObjectFormat(v) {
			case config.SHA1, config.SHA256:
			default:
				return &pktline.ErrorLine{Text: fmt.Sprintf("unknown object format '%s'", escapeClientValue(v))}
			}
		}
		client = vals[len(vals)-1]
	}
	if client != repo.String() {
		return &pktline.ErrorLine{Text: fmt.Sprintf("mismatched object format: server %s; client %s", repo, client)}
	}
	return nil
}

// rejectWithErrorLine sends e to the client as an ERR packet and returns it.
func rejectWithErrorLine(w io.Writer, e *pktline.ErrorLine) error {
	if err := e.Encode(w); err != nil {
		return errors.Join(e, err)
	}
	return e
}

// rejectReceivePack sends e to a receive-pack client and returns it. A client
// that asked for side-band-64k or side-band demultiplexes the response, so e
// goes on the error band followed by a flush, as git's packet_writer_error
// sends a fatal error to a side-band client; any other client gets an ERR
// packet.
func rejectReceivePack(w io.Writer, caps capability.List, e *pktline.ErrorLine) error {
	var mux *sideband.Muxer
	switch {
	case caps.Supports(capability.Sideband64k):
		mux = sideband.NewMuxer(sideband.Sideband64k, w)
	case caps.Supports(capability.Sideband):
		mux = sideband.NewMuxer(sideband.Sideband, w)
	default:
		return rejectWithErrorLine(w, e)
	}
	if _, err := mux.WriteChannel(sideband.ErrorMessage, []byte(e.Text)); err != nil {
		return errors.Join(e, err)
	}
	if err := pktline.WriteFlush(w); err != nil {
		return errors.Join(e, err)
	}
	return e
}
