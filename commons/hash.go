package commons

import (
	"crypto/sha1"
	"fmt"
	"strconv"
)

func GitHash(t string, b []byte) string {
	h := []byte(t)
	h = append(h, ' ')
	h = strconv.AppendInt(h, int64(len(b)), 10)
	h = append(h, 0)
	h = append(h, b...)

	return fmt.Sprintf("%x", sha1.Sum(h))
}
