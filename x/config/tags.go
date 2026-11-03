package config

import (
	"reflect"
	"strings"
	"sync"
)

const (
	tagName       = "gitconfig"
	tagDefault    = "gitconfigDefault"
	tagSubsection = "gitconfigSub"
)

type fieldTag struct {
	key        string
	omitempty  bool
	multivalue bool
	subsection bool
	skip       bool
}

func parseTag(tag string) fieldTag {
	if tag == "-" {
		return fieldTag{skip: true}
	}

	var ft fieldTag
	parts := strings.Split(tag, ",")

	if len(parts) > 0 {
		ft.key = parts[0]
	}

	for _, opt := range parts[1:] {
		switch opt {
		case "omitempty":
			ft.omitempty = true
		case "multivalue":
			ft.multivalue = true
		case "subsection":
			ft.subsection = true
		}
	}

	return ft
}

type structField struct {
	index        []int
	tag          fieldTag
	typ          reflect.Type
	isPtr        bool
	elemType     reflect.Type
	defaultValue string
	hasDefault   bool
	subName      string
}

type structInfo struct {
	fields []structField
}

var (
	structCacheMu sync.RWMutex
	structCache   = map[reflect.Type]*structInfo{}
)

func getStructInfo(t reflect.Type) *structInfo {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	structCacheMu.RLock()
	if info, ok := structCache[t]; ok {
		structCacheMu.RUnlock()
		return info
	}
	structCacheMu.RUnlock()

	info := buildStructInfo(t)

	structCacheMu.Lock()
	structCache[t] = info
	structCacheMu.Unlock()

	return info
}

func buildStructInfo(t reflect.Type) *structInfo {
	info := &structInfo{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		tag := f.Tag.Get(tagName)
		if tag == "" {
			continue
		}

		ft := parseTag(tag)
		if ft.skip {
			continue
		}

		sf := structField{
			index: f.Index,
			tag:   ft,
			typ:   f.Type,
		}

		if f.Type.Kind() == reflect.Pointer {
			sf.isPtr = true
			sf.elemType = f.Type.Elem()
		} else {
			sf.elemType = f.Type
		}

		if dv, ok := f.Tag.Lookup(tagDefault); ok {
			sf.defaultValue = dv
			sf.hasDefault = true
		}

		if sn := f.Tag.Get(tagSubsection); sn != "" {
			sf.subName = sn
		}

		info.fields = append(info.fields, sf)
	}

	return info
}
