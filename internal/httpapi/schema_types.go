package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type schema = map[string]any

// wireSchema describes encoding/json's field shape. Semantic constraints are
// added explicitly in OpenAPI; arbitrary native JSON remains lossless JSON.
func wireSchema(value any) schema { return typeSchema(reflect.TypeOf(value)) }
func typeSchema(t reflect.Type) schema {
	if t == reflect.TypeFor[json.RawMessage]() {
		return schema{}
	}
	if t == reflect.TypeFor[time.Time]() {
		return schema{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		return schema{"anyOf": []any{typeSchema(t.Elem()), schema{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.String:
		return schema{"type": "string"}
	case reflect.Bool:
		return schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return schema{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer", "minimum": 0}
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}
	case reflect.Slice:
		return schema{"type": []string{"array", "null"}, "items": typeSchema(t.Elem())}
	case reflect.Map:
		return schema{"type": []string{"object", "null"}, "additionalProperties": typeSchema(t.Elem())}
	case reflect.Struct:
		props, required := schema{}, []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			if field.Anonymous && tag[0] == "" {
				embedded := typeSchema(field.Type)
				for name, value := range embedded["properties"].(schema) {
					props[name] = value
				}
				if fields, ok := embedded["required"].([]string); ok {
					required = append(required, fields...)
				}
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			props[name] = typeSchema(field.Type)
			if !strings.Contains(field.Tag.Get("json"), ",omitempty") {
				required = append(required, name)
			}
		}
		out := schema{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	default:
		return schema{}
	}
}

func ref(name string) schema { return schema{"$ref": "#/components/schemas/" + name} }
func object(props schema, required ...string) schema {
	s := schema{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func array(items schema) schema { return schema{"type": []string{"array", "null"}, "items": items} }
func strict(s schema) schema {
	s["additionalProperties"] = false
	return s
}
func field(s schema, name string, value schema) { s["properties"].(schema)[name] = value }
func parameter(name, where string, s schema) schema {
	return schema{"name": name, "in": where, "required": where == "path", "schema": s}
}
