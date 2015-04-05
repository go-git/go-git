package packfile

const delta_size_min = 4

func deltaHeaderSize(b []byte) (uint, []byte) {
	var size, j uint
	var cmd byte
	for {
		cmd = b[j]
		size |= (uint(cmd) & 0x7f) << (j * 7)
		j++
		if uint(cmd)&0xb80 == 0 || j == uint(len(b)) {
			break
		}
	}
	return size, b[j:]
}

func PatchDelta(src, delta []byte) []byte {
	if len(delta) < delta_size_min {
		return nil
	}
	size, delta := deltaHeaderSize(delta)
	if size != uint(len(src)) {
		return nil
	}
	size, delta = deltaHeaderSize(delta)
	origSize := size

	dest := make([]byte, 0)

	// var offset uint
	var cmd byte
	for {
		cmd = delta[0]
		delta = delta[1:]
		if (cmd & 0x80) != 0 {
			var cp_off, cp_size uint
			if (cmd & 0x01) != 0 {
				cp_off = uint(delta[0])
				delta = delta[1:]
			}
			if (cmd & 0x02) != 0 {
				cp_off |= uint(delta[0]) << 8
				delta = delta[1:]
			}
			if (cmd & 0x04) != 0 {
				cp_off |= uint(delta[0]) << 16
				delta = delta[1:]
			}
			if (cmd & 0x08) != 0 {
				cp_off |= uint(delta[0]) << 24
				delta = delta[1:]
			}

			if (cmd & 0x10) != 0 {
				cp_size = uint(delta[0])
				delta = delta[1:]
			}
			if (cmd & 0x20) != 0 {
				cp_size |= uint(delta[0]) << 8
				delta = delta[1:]
			}
			if (cmd & 0x40) != 0 {
				cp_size |= uint(delta[0]) << 16
				delta = delta[1:]
			}
			if cp_size == 0 {
				cp_size = 0x10000
			}
			if cp_off+cp_size < cp_off ||
				cp_off+cp_size > uint(len(src)) ||
				cp_size > origSize {
				break
			}
			dest = append(dest, src[cp_off:cp_off+cp_size]...)
			size -= cp_size
		} else if cmd != 0 {
			if uint(cmd) > origSize {
				break
			}
			dest = append(dest, delta[0:uint(cmd)]...)
			size -= uint(cmd)
			delta = delta[uint(cmd):]
		} else {
			return nil
		}
		if size <= 0 {
			break
		}
	}
	return dest
}
