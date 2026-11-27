package config

import (
	"encoding"
	"fmt"
	"reflect"

	format "github.com/go-git/go-git/v6/plumbing/format/config"
)

// Unmarshal reads fields from a parsed Git config into a struct.
// v must be a non-nil pointer to a struct.
//
// The raw *format.Config is not modified. Unknown sections and keys
// remain in raw for round-trip fidelity — the caller owns the
// raw config lifecycle.
func Unmarshal(raw *format.Config, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("gitconfig: Unmarshal requires a non-nil pointer, got %T", v)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("gitconfig: Unmarshal requires a pointer to a struct, got pointer to %s", rv.Kind())
	}

	return decodeStruct(rv, raw)
}

func decodeStruct(rv reflect.Value, raw *format.Config) error {
	info := getStructInfo(rv.Type())

	for i := range info.fields {
		sf := &info.fields[i]
		fv := rv.FieldByIndex(sf.index)
		sectionName := sf.tag.key

		if sf.tag.subsection && isMapOfPtrStruct(sf.typ) {
			if err := decodeSubsectionMap(fv, raw, sectionName); err != nil {
				return fmt.Errorf("gitconfig: %s: %w", sectionName, err)
			}
			continue
		}

		if sf.tag.subsection && sf.isPtr && sf.elemType.Kind() == reflect.Struct {
			if err := decodeSingleSubsection(fv, raw, sectionName, sf.subName); err != nil {
				return err
			}
			continue
		}

		if sf.elemType.Kind() == reflect.Struct && !implementsUnmarshaler(sf.typ) {
			if err := decodeSectionFields(fv, raw, sectionName); err != nil {
				return err
			}
			continue
		}
	}

	return nil
}

func decodeSectionFields(fv reflect.Value, raw *format.Config, sectionName string) error {
	if !raw.HasSection(sectionName) {
		return nil
	}

	section := raw.Section(sectionName)

	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		fv = fv.Elem()
	}

	return decodeOptionsInto(fv, section.Options, sectionName)
}

func decodeOptionsInto(rv reflect.Value, opts format.Options, path string) error {
	info := getStructInfo(rv.Type())

	for i := range info.fields {
		sf := &info.fields[i]
		fv := rv.FieldByIndex(sf.index)
		if err := decodeOption(fv, opts, sf, path); err != nil {
			return err
		}
	}

	return nil
}

func decodeOption(fv reflect.Value, opts format.Options, sf *structField, sectionName string) error {
	key := sf.tag.key
	path := sectionName + "." + key

	if sf.tag.multivalue {
		return decodeMultivalue(fv, opts, sf, path)
	}

	if !opts.Has(key) {
		if sf.hasDefault {
			return setFromString(fv, sf.defaultValue, sf, path)
		}
		return nil
	}

	value := opts.Get(key)
	return setFromString(fv, value, sf, path)
}

func decodeMultivalue(fv reflect.Value, opts format.Options, sf *structField, path string) error {
	values := opts.GetAll(sf.tag.key)
	if len(values) == 0 {
		return nil
	}

	sliceType := fv.Type()
	if sliceType.Kind() != reflect.Slice {
		return fmt.Errorf("gitconfig: %s: multivalue requires a slice type, got %s", path, sliceType)
	}
	elemType := sliceType.Elem()
	slice := reflect.MakeSlice(sliceType, 0, len(values))

	for _, val := range values {
		elem := reflect.New(elemType).Elem()
		if err := setScalar(elem, val, path); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem)
	}

	fv.Set(slice)
	return nil
}

func setFromString(fv reflect.Value, s string, sf *structField, path string) error {
	if sf.isPtr {
		ptr := reflect.New(sf.elemType)
		if err := setScalar(ptr.Elem(), s, path); err != nil {
			return err
		}
		fv.Set(ptr)
		return nil
	}
	return setScalar(fv, s, path)
}

func setScalar(fv reflect.Value, s, path string) error {
	addr := fv
	if fv.CanAddr() {
		addr = fv.Addr()
	}

	if addr.CanInterface() {
		if u, ok := addr.Interface().(Unmarshaler); ok {
			if err := u.UnmarshalGitConfig([]byte(s)); err != nil {
				return fmt.Errorf("gitconfig: %s: %w", path, err)
			}
			return nil
		}
		if u, ok := addr.Interface().(encoding.TextUnmarshaler); ok {
			if err := u.UnmarshalText([]byte(s)); err != nil {
				return fmt.Errorf("gitconfig: %s: %w", path, err)
			}
			return nil
		}
	}

	v, err := stringToValue(s, fv.Type())
	if err != nil {
		return fmt.Errorf("gitconfig: %s: %w", path, err)
	}
	fv.Set(v)
	return nil
}

func decodeSubsectionMap(fv reflect.Value, raw *format.Config, sectionName string) error {
	if !raw.HasSection(sectionName) {
		return nil
	}

	section := raw.Section(sectionName)
	if len(section.Subsections) == 0 {
		return nil
	}

	mapType := fv.Type()
	elemType := mapType.Elem().Elem()

	if fv.IsNil() {
		fv.Set(reflect.MakeMap(mapType))
	}

	for _, sub := range section.Subsections {
		newVal := reflect.New(elemType)
		if err := decodeOptionsInto(newVal.Elem(), sub.Options, sectionName+"."+sub.Name); err != nil {
			return err
		}
		fv.SetMapIndex(reflect.ValueOf(sub.Name), newVal)
	}

	return nil
}

func decodeSingleSubsection(fv reflect.Value, raw *format.Config, sectionName, subName string) error {
	if !raw.HasSection(sectionName) {
		return nil
	}

	section := raw.Section(sectionName)
	if !section.HasSubsection(subName) {
		return nil
	}

	sub := section.Subsection(subName)
	newVal := reflect.New(fv.Type().Elem())
	if err := decodeOptionsInto(newVal.Elem(), sub.Options, sectionName+"."+subName); err != nil {
		return err
	}
	fv.Set(newVal)
	return nil
}

func isMapOfPtrStruct(t reflect.Type) bool {
	return t.Kind() == reflect.Map &&
		t.Key().Kind() == reflect.String &&
		t.Elem().Kind() == reflect.Pointer &&
		t.Elem().Elem().Kind() == reflect.Struct
}

func implementsUnmarshaler(t reflect.Type) bool {
	unmarshalerType := reflect.TypeFor[Unmarshaler]()
	textUnmarshalerType := reflect.TypeFor[encoding.TextUnmarshaler]()

	return t.Implements(unmarshalerType) ||
		reflect.PointerTo(t).Implements(unmarshalerType) ||
		t.Implements(textUnmarshalerType) ||
		reflect.PointerTo(t).Implements(textUnmarshalerType)
}
