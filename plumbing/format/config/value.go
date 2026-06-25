package config

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Errors returned by the value parsers. They mirror the failure modes of
// git's parse.c (invalid syntax vs. out-of-range).
var (
	// ErrInvalidValue is returned when a value cannot be parsed as the
	// requested type.
	ErrInvalidValue = errors.New("invalid config value")
	// ErrValueOutOfRange is returned when a numeric value (including its
	// unit factor) does not fit in the requested type.
	ErrValueOutOfRange = errors.New("config value out of range")
)

// ParseBool parses value as a Git boolean, mirroring git's
// git_parse_maybe_bool (parse.c). The tokens "true", "yes" and "on" are true
// and "false", "no" and "off" are false, all case-insensitively. Any value
// that parses as an integer is true when non-zero. A bare key, which go-git
// represents as an empty value, is reported as true. Unrecognised values
// return ErrInvalidValue.
//
// Reference: https://github.com/git/git/blob/master/parse.c git_parse_maybe_bool
func ParseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	case "":
		// git treats a valueless key (NULL) as true. go-git collapses a
		// bare key and an explicit empty value to "", and the bare-key
		// shorthand ([core] bare) is the meaningful real-world case.
		return true, nil
	}

	if i, err := ParseInt64(value); err == nil {
		return i != 0, nil
	}

	return false, ErrInvalidValue
}

// ParseInt64 parses value as a Git integer, mirroring git's git_parse_signed
// (parse.c): the number is read with strconv base 0 (so 0x and 0 prefixes are
// honoured) and may carry a single case-insensitive unit suffix of k, m or g
// for 1024, 1024^2 or 1024^3 respectively.
//
// Reference: https://github.com/git/git/blob/master/parse.c git_parse_signed
func ParseInt64(value string) (int64, error) {
	if value == "" {
		return 0, ErrInvalidValue
	}

	num, factor, err := splitUnit(value)
	if err != nil {
		return 0, err
	}

	val, err := strconv.ParseInt(num, 0, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, ErrValueOutOfRange
		}
		return 0, ErrInvalidValue
	}

	if factor != 1 {
		if val > math.MaxInt64/factor || val < math.MinInt64/factor {
			return 0, ErrValueOutOfRange
		}
		val *= factor
	}

	return val, nil
}

// ParseInt parses value as a Git integer and reports ErrValueOutOfRange when
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

// ParseUint parses value as a non-negative Git integer with the same unit
// suffixes as ParseInt64. Negative values are rejected, matching git's
// git_parse_unsigned.
func ParseUint(value string) (uint64, error) {
	if value == "" {
		return 0, ErrInvalidValue
	}
	if strings.ContainsRune(value, '-') {
		return 0, ErrInvalidValue
	}

	num, factor, err := splitUnit(value)
	if err != nil {
		return 0, err
	}

	val, err := strconv.ParseUint(num, 0, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, ErrValueOutOfRange
		}
		return 0, ErrInvalidValue
	}

	if factor != 1 {
		if val > math.MaxUint64/uint64(factor) {
			return 0, ErrValueOutOfRange
		}
		val *= uint64(factor)
	}

	return val, nil
}

// splitUnit separates the numeric part of value from an optional trailing
// k/m/g unit suffix and returns the corresponding 1024-based factor.
func splitUnit(value string) (num string, factor int64, err error) {
	if value == "" {
		return "", 0, ErrInvalidValue
	}

	switch last := value[len(value)-1]; last {
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
