package config

import (
	"encoding"
	"fmt"
	"reflect"
	"sort"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Marshal writes struct fields into a parsed Git config.
// Existing keys in raw are updated; new keys are added.
// Unknown keys already in raw are preserved.
func Marshal(v any, raw *format.Config) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("gitconfig: Marshal requires a struct, got %s", rv.Kind())
	}

	return encodeStruct(rv, raw)
}

func encodeStruct(rv reflect.Value, raw *format.Config) error {
	info := getStructInfo(rv.Type())

	for i := range info.fields {
		sf := &info.fields[i]
		fv := rv.FieldByIndex(sf.index)
		sectionName := sf.tag.key

		if sf.tag.subsection && isMapOfPtrStruct(sf.typ) {
			if err := encodeSubsectionMap(fv, raw, sectionName); err != nil {
				return err
			}
			continue
		}

		if sf.tag.subsection && sf.isPtr && sf.elemType.Kind() == reflect.Struct {
			if err := encodeSingleSubsection(fv, raw, sectionName, sf.subName); err != nil {
				return err
			}
			continue
		}

		if sf.elemType.Kind() == reflect.Struct && !implementsMarshaler(sf.typ) {
			if err := encodeSectionFields(fv, raw, sectionName); err != nil {
				return err
			}
			continue
		}
	}

	return nil
}

func encodeSectionFields(fv reflect.Value, raw *format.Config, sectionName string) error {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}

	section := raw.Section(sectionName)
	return encodeOptionsFrom(fv, section, sectionName)
}

func encodeOptionsFrom(rv reflect.Value, section *format.Section, sectionName string) error {
	info := getStructInfo(rv.Type())

	for i := range info.fields {
		sf := &info.fields[i]
		fv := rv.FieldByIndex(sf.index)
		if err := encodeOption(section, fv, sf, sectionName); err != nil {
			return err
		}
	}

	return nil
}

func encodeOption(section *format.Section, fv reflect.Value, sf *structField, sectionName string) error {
	key := sf.tag.key

	if sf.tag.multivalue {
		return encodeMultivalue(section, fv, sf, sectionName)
	}

	if sf.isPtr {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}

	if sf.tag.omitempty && isZero(fv) {
		return nil
	}

	s, err := marshalValue(fv)
	if err != nil {
		return fmt.Errorf("gitconfig: %s.%s: %w", sectionName, key, err)
	}

	section.SetOption(key, s)
	return nil
}

func encodeMultivalue(section *format.Section, fv reflect.Value, sf *structField, sectionName string) error {
	key := sf.tag.key

	if fv.Kind() != reflect.Slice || fv.Len() == 0 {
		return nil
	}

	section.RemoveOption(key)
	for i := range fv.Len() {
		s, err := marshalValue(fv.Index(i))
		if err != nil {
			return fmt.Errorf("gitconfig: %s.%s[%d]: %w", sectionName, key, i, err)
		}
		section.AddOption(key, s)
	}
	return nil
}

func encodeSubsectionMap(fv reflect.Value, raw *format.Config, sectionName string) error {
	if fv.IsNil() || fv.Len() == 0 {
		return nil
	}

	section := raw.Section(sectionName)
	keys := sortedMapKeys(fv)

	for _, name := range keys {
		val := fv.MapIndex(reflect.ValueOf(name))
		if val.IsNil() {
			continue
		}

		sub := section.Subsection(name)
		if err := encodeSubsectionOptionsFrom(val.Elem(), sub, sectionName+"."+name); err != nil {
			return err
		}
	}

	return nil
}

func encodeSingleSubsection(fv reflect.Value, raw *format.Config, sectionName, subName string) error {
	if fv.IsNil() {
		return nil
	}

	section := raw.Section(sectionName)
	sub := section.Subsection(subName)
	return encodeSubsectionOptionsFrom(fv.Elem(), sub, sectionName+"."+subName)
}

func encodeSubsectionOptionsFrom(rv reflect.Value, sub *format.Subsection, path string) error {
	info := getStructInfo(rv.Type())

	for i := range info.fields {
		sf := &info.fields[i]
		fv := rv.FieldByIndex(sf.index)
		key := sf.tag.key

		if sf.tag.multivalue {
			if fv.Kind() != reflect.Slice || fv.Len() == 0 {
				continue
			}
			values := make([]string, 0, fv.Len())
			for j := range fv.Len() {
				s, err := marshalValue(fv.Index(j))
				if err != nil {
					return fmt.Errorf("gitconfig: %s.%s[%d]: %w", path, key, j, err)
				}
				values = append(values, s)
			}
			sub.SetOption(key, values...)
			continue
		}

		if sf.isPtr {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		if sf.tag.omitempty && isZero(fv) {
			continue
		}

		s, err := marshalValue(fv)
		if err != nil {
			return fmt.Errorf("gitconfig: %s.%s: %w", path, key, err)
		}
		sub.SetOption(key, s)
	}

	return nil
}

func marshalValue(fv reflect.Value) (string, error) {
	if fv.CanInterface() {
		if m, ok := fv.Interface().(Marshaler); ok {
			return m.MarshalGitConfig()
		}
		if m, ok := fv.Interface().(encoding.TextMarshaler); ok {
			b, err := m.MarshalText()
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
	}

	if fv.CanAddr() {
		addr := fv.Addr()
		if m, ok := addr.Interface().(Marshaler); ok {
			return m.MarshalGitConfig()
		}
		if m, ok := addr.Interface().(encoding.TextMarshaler); ok {
			b, err := m.MarshalText()
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
	}

	return valueToString(fv)
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Map:
		return v.IsNil() || v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Struct:
		return v.IsZero()
	default:
		return false
	}
}

func implementsMarshaler(t reflect.Type) bool {
	marshalerType := reflect.TypeFor[Marshaler]()
	textMarshalerType := reflect.TypeFor[encoding.TextMarshaler]()

	return t.Implements(marshalerType) ||
		reflect.PointerTo(t).Implements(marshalerType) ||
		t.Implements(textMarshalerType) ||
		reflect.PointerTo(t).Implements(textMarshalerType)
}

func sortedMapKeys(m reflect.Value) []string {
	keys := make([]string, 0, m.Len())
	for _, k := range m.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	return keys
}
