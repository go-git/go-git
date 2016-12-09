package packfile

import (
	"io/ioutil"

	"gopkg.in/src-d/go-git.v4/plumbing"
)

// See https://github.com/jelmer/dulwich/blob/master/dulwich/pack.py and
// https://github.com/tarruda/node-git-core/blob/master/src/js/delta.js
// for more info

const (
	maxCopyLen = 0xffff
)

// GetDelta returns the way of how to transform base object to target object
func GetDelta(base, target plumbing.Object) ([]byte, error) {
	baseReader, err := base.Reader()
	if err != nil {
		return nil, err
	}
	targetReader, err := target.Reader()
	if err != nil {
		return nil, err
	}

	baseBuf, err := ioutil.ReadAll(baseReader)
	if err != nil {
		return nil, err
	}

	targetBuf, err := ioutil.ReadAll(targetReader)
	if err != nil {
		return nil, err
	}

	return DiffDelta(baseBuf, targetBuf), nil
}

// DiffDelta returns the way of how to transform baseBuf to targetBuf
func DiffDelta(baseBuf []byte, targetBuf []byte) []byte {
	var outBuff []byte

	outBuff = append(outBuff, deltaEncodeSize(len(baseBuf))...)
	outBuff = append(outBuff, deltaEncodeSize(len(targetBuf))...)

	sm := newMatcher(baseBuf, targetBuf)
	for _, op := range sm.GetOpCodes() {
		switch {
		case op.Tag == tagEqual:
			copyStart := op.I1
			copyLen := op.I2 - op.I1
			for {
				if copyLen <= 0 {
					break
				}
				var toCopy int
				if copyLen < maxCopyLen {
					toCopy = copyLen
				} else {
					toCopy = maxCopyLen
				}

				outBuff = append(outBuff, encodeCopyOperation(copyStart, toCopy)...)
				copyStart += toCopy
				copyLen -= toCopy
			}
		case op.Tag == tagReplace || op.Tag == tagInsert:
			s := op.J2 - op.J1
			o := op.J1
			for {
				if s <= 127 {
					break
				}
				outBuff = append(outBuff, byte(127))
				outBuff = append(outBuff, targetBuf[o:o+127]...)
				s -= 127
				o += 127
			}
			outBuff = append(outBuff, byte(s))
			outBuff = append(outBuff, targetBuf[o:o+s]...)
		}
	}

	return outBuff
}

func deltaEncodeSize(size int) []byte {
	var ret []byte
	c := size & 0x7f
	size >>= 7
	for {
		if size == 0 {
			break
		}

		ret = append(ret, byte(c|0x80))
		c = size & 0x7f
		size >>= 7
	}
	ret = append(ret, byte(c))

	return ret
}

func encodeCopyOperation(offset, length int) []byte {
	code := 0x80
	var opcodes []byte

	if offset&0xff != 0 {
		opcodes = append(opcodes, byte(offset&0xff))
		code |= 0x01
	}

	if offset&0xff00 != 0 {
		opcodes = append(opcodes, byte((offset&0xff00)>>8))
		code |= 0x02
	}

	if offset&0xff0000 != 0 {
		opcodes = append(opcodes, byte((offset&0xff0000)>>16))
		code |= 0x04
	}

	if offset&0xff000000 != 0 {
		opcodes = append(opcodes, byte((offset&0xff000000)>>24))
		code |= 0x08
	}

	if length&0xff != 0 {
		opcodes = append(opcodes, byte(length&0xff))
		code |= 0x10
	}

	if length&0xff00 != 0 {
		opcodes = append(opcodes, byte((length&0xff00)>>8))
		code |= 0x20
	}

	if length&0xff0000 != 0 {
		opcodes = append(opcodes, byte((length&0xff0000)>>16))
		code |= 0x40
	}

	return append([]byte{byte(code)}, opcodes...)
}
