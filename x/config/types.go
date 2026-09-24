package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0", "":
		return false, nil
	default:
		return false, fmt.Errorf("cannot parse %q as bool", s)
	}
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func stringToValue(s string, t reflect.Type) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.String:
		return reflect.ValueOf(s).Convert(t), nil

	case reflect.Bool:
		v, err := parseBool(s)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v).Convert(t), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("cannot parse %q as %s: %w", s, t, err)
		}
		return reflect.ValueOf(v).Convert(t), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("cannot parse %q as %s: %w", s, t, err)
		}
		return reflect.ValueOf(v).Convert(t), nil

	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("cannot parse %q as %s: %w", s, t, err)
		}
		return reflect.ValueOf(v).Convert(t), nil

	default:
		return reflect.Value{}, fmt.Errorf("unsupported type %s", t)
	}
}

func valueToString(v reflect.Value) (string, error) {
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil

	case reflect.Bool:
		return formatBool(v.Bool()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil

	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, v.Type().Bits()), nil

	default:
		return "", fmt.Errorf("unsupported type %s", v.Type())
	}
}
