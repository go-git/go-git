package config

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Errors returned by the value parsers. They mirror the failure modes of git's
// parse.c: a value that is syntactically not of the requested type, or a
// numeric value (including its unit factor) that does not fit the target type.
var (
	// ErrInvalidBool is returned when a value cannot be parsed as a git boolean.
	ErrInvalidBool = errors.New("config: invalid boolean value")
	// ErrInvalidNumber is returned when a value cannot be parsed as a git integer.
	ErrInvalidNumber = errors.New("config: invalid numeric value")
	// ErrValueOutOfRange is returned when a numeric value does not fit the
	// requested type.
	ErrValueOutOfRange = errors.New("config: numeric value out of range")
)

// ParseBool parses value as a git boolean, mirroring git_parse_maybe_bool in
// git's parse.c. The tokens "true", "yes" and "on" are true and "false", "no"
// and "off" are false, all case-insensitively. Any value that parses as an
// integer is true when non-zero. An empty value is treated as true: go-git
// stores a valueless ("bare") key such as "[core] bare" with an empty value,
// and that shorthand means true. Unrecognised values return ErrInvalidBool.
//
// Reference: https://github.com/git/git/blob/master/parse.c (git_parse_maybe_bool)
func ParseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	case "":
		return true, nil
	}

	if i, err := ParseInt64(value); err == nil {
		return i != 0, nil
	}

	return false, ErrInvalidBool
}

// ParseInt64 parses value as a git integer, mirroring git_parse_signed in git's
// parse.c: the number is read with strconv base 0 (so 0x hexadecimal and 0
// octal prefixes are honoured) and may carry a single case-insensitive unit
// suffix of k, m or g for 1024, 1024^2 or 1024^3 respectively.
//
// Reference: https://github.com/git/git/blob/master/parse.c (git_parse_signed)
func ParseInt64(value string) (int64, error) {
	num, factor, err := splitUnit(value)
	if err != nil {
		return 0, err
	}

	val, err := strconv.ParseInt(num, 0, 64)
	if err != nil {
		return 0, numericError(err)
	}

	if factor != 1 {
		if val > math.MaxInt64/factor || val < math.MinInt64/factor {
			return 0, ErrValueOutOfRange
		}
		val *= factor
	}

	return val, nil
}

// ParseInt parses value as a git integer and reports ErrValueOutOfRange when
// the result does not fit in an int.
func ParseInt(value string) (int, error) {
	v, err := ParseInt64(value)
	if err != nil {
		return 0, err
	}
	if v > math.MaxInt || v < math.MinInt {
		return 0, ErrValueOutOfRange
	}
	return int(v), nil
}

// ParseUint64 parses value as a non-negative git integer with the same unit
// suffixes as ParseInt64. Negative values are rejected, mirroring
// git_parse_unsigned.
//
// Reference: https://github.com/git/git/blob/master/parse.c (git_parse_unsigned)
func ParseUint64(value string) (uint64, error) {
	if strings.ContainsRune(value, '-') {
		return 0, ErrInvalidNumber
	}

	num, factor, err := splitUnit(value)
	if err != nil {
		return 0, err
	}

	val, err := strconv.ParseUint(num, 0, 64)
	if err != nil {
		return 0, numericError(err)
	}

	if factor != 1 {
		ufactor := uint64(factor)
		if val > math.MaxUint64/ufactor {
			return 0, ErrValueOutOfRange
		}
		val *= ufactor
	}

	return val, nil
}

// ParseUint parses value as a non-negative git integer and reports
// ErrValueOutOfRange when the result does not fit in a uint.
func ParseUint(value string) (uint, error) {
	v, err := ParseUint64(value)
	if err != nil {
		return 0, err
	}
	if v > math.MaxUint {
		return 0, ErrValueOutOfRange
	}
	return uint(v), nil
}

// splitUnit separates the numeric part of value from an optional trailing
// k/m/g unit suffix and returns the corresponding 1024-based factor.
func splitUnit(value string) (num string, factor int64, err error) {
	if value == "" {
		return "", 0, ErrInvalidNumber
	}

	switch value[len(value)-1] {
	case 'k', 'K':
		return value[:len(value)-1], 1024, nil
	case 'm', 'M':
		return value[:len(value)-1], 1024 * 1024, nil
	case 'g', 'G':
		return value[:len(value)-1], 1024 * 1024 * 1024, nil
	default:
		return value, 1, nil
	}
}

func numericError(err error) error {
	if errors.Is(err, strconv.ErrRange) {
		return ErrValueOutOfRange
	}
	return ErrInvalidNumber
}
