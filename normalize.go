package main

import (
	"bytes"
	"encoding/json"
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
// casing (lowerCamelCase), null/empty fields pruned, and map keys sorted.
//
// The aws-sdk-go-v2 types carry no json tags, so encoding/json emits PascalCase
// keys; lowerKeys converts them back to the API's lowerCamelCase shape.
func canonicalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", err
	}
	raw = prune(lowerKeys(raw))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(raw); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// lowerKeys recursively lowercases the first letter of every map key.
func lowerKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[lowerFirst(k)] = lowerKeys(val)
		}
		return m
	case []any:
		for i := range t {
			t[i] = lowerKeys(t[i])
		}
		return t
	default:
		return v
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
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
