package packfile

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

var packFileWithEmptyObjects = "UEFDSwAAAAIAAAALnw54nKXMQWoDMQxA0b1PoX2hSLIm44FSAlmXnEG2NYlhXAfHgdLb5Cy9WAM5Qpb/Lf7oZqArUpakyYtQjCoxZ5lmWXwwyuzJbHqAuYt2+x6QoyCyhYCKIa67lGameSLWvPh5JU0hsCg7vY1z6/D1d/8ptcHhprm3Kxz7KL/wUdOz96eqZXtPrX4CCeOOPU8Eb0iI7qG1jGGvXdxaNoPs/gHeNkp8lA94nKXMQUpDMRCA4X1OMXtBZpI3L3kiRXAtPcMkmWjgxZSYQultPEsv1oJHcPl/i38OVRC0IXF0lshrJorZEcpKmTEJYbA+B3aFzEmGfk9gpqJEsmnZNutXF71i1IURU/G0bsWWwJ6NnOdXH/Bx+73U1uH9LHn0HziOWa/w2tJfv302qftz6u0AtFh0wQdmeEJCNA9tdU7938WUuivEF5CczR11ZEsNnw54nKWMUQoCIRRF/13F+w/ijY6jQkTQd7SGpz5LyAxzINpNa2ljTbSEPu/hnNsbM4TJTzqyt561GdUUmJKT6K2MeiCVgnZWoY/iRo2vHVS0URrUS+e+dkqIEp11HMhh9IaUkRM6QXM/1waH9+uRS4X9TLHVOxxbz0/YlPDbu1OhfFmHWrYwjBKVNVaNsMIBUSy05N75vxeR8oXBiw8GoErCnwt4nKXMzQkCMRBA4XuqmLsgM2M2ZkAWwbNYQ341sCEQsyB2Yy02pmAJHt93eKOnBFpMNJqtl5CFxVIMomViomQSEWP2JrN3yq3j1jqc369HqQ1Oq4u93eHSR3nCoYZfH6/VlWUbWp2BNOPO7i1OsEFCVF+tZYz030XlsiRw6gPZ0jxaqwV4nDM0MDAzMVFIZHg299HsTRevOXt3a64rj7px6ElP8ERDiGQSQ2uoXe8RrcodS5on+J4/u8HjD4NDKFQyRS8tPx+rbgDt3yiEMHicAwAAAAABPnicS0wEAa4kMOACACTjBKdkZXici7aaYAUAA3gBYKoDeJwzNDAwMzFRSGR4NvfR7E0Xrzl7d2uuK4+6cehJT/BEQ4hkEsOELYFJvS2eX47UJdVttFQrenrmzQwA13MaiDd4nEtMBAEuAApMAlGtAXicMzQwMDMxUUhkeDb30exNF685e3drriuPunHoSU/wRACvkA258N/i8hVXx9CiAZzvFXNIhCuSFmE="

func TestReadPackfile(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString(packFileWithEmptyObjects)
	d := bytes.NewReader(data)

	r, err := NewPackfileReader(d, nil)
	assert.Nil(t, err)

	p, err := r.Read()
	assert.Nil(t, err)

	assert.Equal(t, 11, p.ObjectCount)
	assert.Equal(t, 4, len(p.Commits))
	assert.Equal(t, 4, len(p.Trees))
}

func TestReadPackfileInvalid(t *testing.T) {
	r, err := NewPackfileReader(bytes.NewReader([]byte("dasdsadasas")), nil)
	assert.Nil(t, err)

	_, err = r.Read()
	_, ok := err.(*ReaderError)
	assert.True(t, ok)
}
