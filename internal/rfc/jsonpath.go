package rfc

import (
	"fmt"
	"strings"
)

// extractField traverses a parsed JSON structure using a dot-separated path.
// For example, extractField(data, "data.status") navigates into
// data["data"]["status"]. When traversal hits a []interface{}, the first
// element is automatically unwrapped.
func extractField(data interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}

	parts := strings.Split(path, ".")
	current := data

	for _, key := range parts {
		// Auto-unwrap arrays to first element
		if arr, ok := current.([]interface{}); ok {
			if len(arr) == 0 {
				return nil, fmt.Errorf("empty array at path segment %q", key)
			}
			current = arr[0]
		}

		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object at path segment %q, got %T", key, current)
		}

		val, exists := m[key]
		if !exists {
			return nil, fmt.Errorf("field %q not found", key)
		}
		current = val
	}

	return current, nil
}

// extractStringField extracts a value at the given dot-path and converts it to
// a string. Handles string, bool, float64 (JSON numbers), and nil values.
func extractStringField(data interface{}, path string) (string, error) {
	val, err := extractField(data, path)
	if err != nil {
		return "", err
	}

	switch v := val.(type) {
	case string:
		return v, nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), nil
		}
		return fmt.Sprintf("%g", v), nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
