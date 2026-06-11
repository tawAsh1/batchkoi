package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
)

// remoteToInput converts a registered JobDefinition into the equivalent
// RegisterJobDefinitionInput by round-tripping through JSON. The input type
// lacks the server-managed fields (jobDefinitionArn, revision, status,
// containerOrchestrationType), so they are dropped automatically.
func remoteToInput(jd *types.JobDefinition) (*batch.RegisterJobDefinitionInput, error) {
	b, err := json.Marshal(jd)
	if err != nil {
		return nil, err
	}
	var in batch.RegisterJobDefinitionInput
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	return &in, nil
}

// canonicalJSON renders a value to a stable JSON form for diffing: AWS API key
// casing (lowerCamelCase) for struct fields, user-data map keys untouched,
// order-insensitive lists sorted, null/empty fields pruned, and object keys
// sorted (by the JSON encoder).
func canonicalJSON(v any) (string, error) {
	raw := prune(sortCanonical(apiValue(reflect.ValueOf(v))))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(raw); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// apiValue converts an aws-sdk-go-v2 value into a generic JSON tree with the
// API's key casing. The SDK types carry no json tags, so a plain marshal emits
// PascalCase struct fields; we lowercase those while walking the *typed*
// value, which is what lets us tell schema keys from data: struct field names
// become lowerCamelCase, but map keys (tags, parameters, logConfiguration
// options, EKS labels, ...) are user data and pass through verbatim.
func apiValue(rv reflect.Value) any {
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return apiValue(rv.Elem())
	case reflect.Struct:
		m := make(map[string]any, rv.NumField())
		t := rv.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" { // unexported (noSmithyDocumentSerde)
				continue
			}
			m[lowerFirst(f.Name)] = apiValue(rv.Field(i))
		}
		return m
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		m := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			m[iter.Key().String()] = apiValue(iter.Value())
		}
		return m
	case reflect.Slice:
		if rv.IsNil() {
			return nil
		}
		fallthrough
	case reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = apiValue(rv.Index(i))
		}
		return out
	case reflect.String: // includes enum types like types.JobDefinitionType
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	default:
		return rv.Interface()
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// orderInsensitiveLists are schema keys whose list order is not significant to
// AWS Batch, so a pure reordering must not show up as a diff. EKS `env` is
// deliberately NOT here: Kubernetes resolves $(VAR) references in order.
var orderInsensitiveLists = map[string]bool{
	"environment":   true,
	"secrets":       true,
	"secretOptions": true,
}

// sortCanonical sorts name-keyed lists under order-insensitive schema keys.
// Only lists of objects carrying a string "name" are touched, so string-typed
// user data (tag/parameter values) can never match.
func sortCanonical(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			val = sortCanonical(val)
			if list, ok := val.([]any); ok && orderInsensitiveLists[k] {
				sort.SliceStable(list, func(i, j int) bool {
					return nameOf(list[i]) < nameOf(list[j])
				})
			}
			t[k] = val
		}
	case []any:
		for i := range t {
			t[i] = sortCanonical(t[i])
		}
	}
	return v
}

func nameOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["name"].(string); ok {
			return s
		}
	}
	return ""
}

// prune removes nulls and empty objects/arrays so that fields the user did not
// set don't show up as diff noise.
func prune(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			pv := prune(val)
			switch pp := pv.(type) {
			case nil:
				continue
			case map[string]any:
				if len(pp) == 0 {
					continue
				}
			case []any:
				if len(pp) == 0 {
					continue
				}
			}
			m[k] = pv
		}
		return m
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			out = append(out, prune(val))
		}
		return out
	default:
		return v
	}
}
