package rfc

import (
	"testing"
)

func TestExtractField_TopLevel(t *testing.T) {
	data := map[string]interface{}{
		"status": "approved",
	}

	val, err := extractField(data, "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "approved" {
		t.Errorf("expected 'approved', got %v", val)
	}
}

func TestExtractField_Nested(t *testing.T) {
	data := map[string]interface{}{
		"data": map[string]interface{}{
			"change": map[string]interface{}{
				"status": "closed",
			},
		},
	}

	val, err := extractField(data, "data.change.status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "closed" {
		t.Errorf("expected 'closed', got %v", val)
	}
}

func TestExtractField_ArrayUnwrap(t *testing.T) {
	data := map[string]interface{}{
		"results": []interface{}{
			map[string]interface{}{
				"id": "first",
			},
			map[string]interface{}{
				"id": "second",
			},
		},
	}

	val, err := extractField(data, "results.id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "first" {
		t.Errorf("expected 'first', got %v", val)
	}
}

func TestExtractField_EmptyArray(t *testing.T) {
	data := map[string]interface{}{
		"results": []interface{}{},
	}

	_, err := extractField(data, "results.id")
	if err == nil {
		t.Fatal("expected error for empty array")
	}
}

func TestExtractField_MissingField(t *testing.T) {
	data := map[string]interface{}{
		"status": "ok",
	}

	_, err := extractField(data, "missing")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestExtractField_EmptyPath(t *testing.T) {
	data := map[string]interface{}{
		"status": "ok",
	}

	val, err := extractField(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatal("expected map for empty path")
	}
	if m["status"] != "ok" {
		t.Error("expected original data returned for empty path")
	}
}

func TestExtractField_NonObjectTraversal(t *testing.T) {
	data := map[string]interface{}{
		"status": "ok",
	}

	_, err := extractField(data, "status.nested")
	if err == nil {
		t.Fatal("expected error when traversing through non-object")
	}
}

func TestExtractStringField_String(t *testing.T) {
	data := map[string]interface{}{"name": "hello"}
	val, err := extractStringField(data, "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %q", val)
	}
}

func TestExtractStringField_Bool(t *testing.T) {
	data := map[string]interface{}{"active": true}
	val, err := extractStringField(data, "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "true" {
		t.Errorf("expected 'true', got %q", val)
	}
}

func TestExtractStringField_Float(t *testing.T) {
	data := map[string]interface{}{"count": float64(42)}
	val, err := extractStringField(data, "count")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "42" {
		t.Errorf("expected '42', got %q", val)
	}
}

func TestExtractStringField_Nil(t *testing.T) {
	data := map[string]interface{}{"value": nil}
	val, err := extractStringField(data, "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for nil, got %q", val)
	}
}
