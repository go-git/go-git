package pktline

import (
	"errors"
	"io"
	"strconv"
)

var (
	ErrUnderflow     = errors.New("unexepected string length")
	ErrInvalidHeader = errors.New("invalid header")
	ErrInvalidLen    = errors.New("invalid length")
)

type Decoder struct {
	r io.Reader
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r}
}

func (d *Decoder) readLine() (string, error) {
	raw := make([]byte, HEADER_LENGTH)
	if _, err := d.r.Read(raw); err != nil {
		return "", err
	}

	header, err := strconv.ParseInt(string(raw), 16, 16)
	if err != nil {
		return "", ErrInvalidHeader
	}

	if header == 0 {
		return "", nil
	}

	exp := int(header - HEADER_LENGTH)
	if exp < 0 {
		return "", ErrInvalidLen
	}

	line := make([]byte, exp)
	if read, err := d.r.Read(line); err != nil {
		return "", err
	} else if read != exp {
		return "", ErrUnderflow
	}

	return string(line), nil
}

func (d *Decoder) ReadLine() (string, error) {
	return d.readLine()
}

func (d *Decoder) ReadBlock() ([]string, error) {
	o := make([]string, 0)

	for {
		line, err := d.readLine()
		if err == io.EOF {
			return o, nil
		}

		if err != nil {
			return o, err
		}

		if err == nil && line == "" {
			return o, nil
		}

		o = append(o, line)
	}

	return o, nil
}

func (d *Decoder) ReadAll() ([]string, error) {
	result, err := d.ReadBlock()
	if err != nil {
		return result, err
	}

	for {
		lines, err := d.ReadBlock()
		if err == io.EOF {
			return result, nil
		}

		if err != nil {
			return result, err
		}

		if err == nil && len(lines) == 0 {
			return result, nil
		}

		result = append(result, lines...)
	}

	return result, nil
}
