package api

import (
	"reflect"
)

// ReflectModel maps each field of a struct (or pointer to struct) to an OpenAPI
// property keyed by its JSON name.
func ReflectModel(model any) map[string]any {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	properties := make(map[string]any)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonName := field.Tag.Get("json")
		if jsonName == "" {
			jsonName = field.Name
		}
		properties[jsonName] = map[string]string{
			"type": mapGoType(field.Type.Kind()),
		}
	}
	return properties
}

func mapGoType(k reflect.Kind) string {
	switch k {
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Bool:
		return "boolean"
	case reflect.Float64, reflect.Float32:
		return "number"
	case reflect.String:
		return "string"
	default:
		return "object"
	}
}
