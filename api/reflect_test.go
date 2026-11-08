package api

import "reflect"

func reflectType(v any) reflect.Type {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Interface {
		return t.Elem()
	}
	return t
}
