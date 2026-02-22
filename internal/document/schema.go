package document

import (
	"fmt"
	"time"
)

// FieldType represents the type of a schema field.
type FieldType int

const (
	FieldTypeString FieldType = iota + 1
	FieldTypeInt
	FieldTypeFloat
	FieldTypeBool
	FieldTypeStringSlice
	FieldTypeIntSlice
	FieldTypeFloatSlice
	FieldTypeDatetime
	FieldTypeReference
)

func (ft FieldType) String() string {
	switch ft {
	case FieldTypeString:
		return "string"
	case FieldTypeInt:
		return "int"
	case FieldTypeFloat:
		return "float"
	case FieldTypeBool:
		return "bool"
	case FieldTypeStringSlice:
		return "[]string"
	case FieldTypeIntSlice:
		return "[]int"
	case FieldTypeFloatSlice:
		return "[]float"
	case FieldTypeDatetime:
		return "datetime"
	case FieldTypeReference:
		return "reference"
	default:
		return "unknown"
	}
}

func ParseFieldType(s string) (FieldType, error) {
	switch s {
	case "string":
		return FieldTypeString, nil
	case "int":
		return FieldTypeInt, nil
	case "float":
		return FieldTypeFloat, nil
	case "bool":
		return FieldTypeBool, nil
	case "[]string":
		return FieldTypeStringSlice, nil
	case "[]int":
		return FieldTypeIntSlice, nil
	case "[]float":
		return FieldTypeFloatSlice, nil
	case "datetime":
		return FieldTypeDatetime, nil
	case "reference":
		return FieldTypeReference, nil
	default:
		return 0, fmt.Errorf("unknown field type: %q", s)
	}
}

func (ft FieldType) MarshalText() ([]byte, error) {
	return []byte(ft.String()), nil
}

func (ft *FieldType) UnmarshalText(data []byte) error {
	parsed, err := ParseFieldType(string(data))
	if err != nil {
		return err
	}
	*ft = parsed
	return nil
}

// FieldDefinition describes a single field in a database schema.
type FieldDefinition struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Indexed     bool      `json:"indexed,omitempty"`
	FullText    bool      `json:"full_text,omitempty"`
	ReferenceDB string    `json:"reference_db,omitempty"`
}

// Schema defines the structure of documents in a database.
type Schema struct {
	Fields              []FieldDefinition `json:"fields"`
	EmbeddingModel      string            `json:"embedding_model,omitempty"`
	EmbeddingDimensions int               `json:"embedding_dimensions,omitempty"`
}

// Validate checks that the given attributes conform to this schema.
func (s *Schema) Validate(attrs map[string]any) error {
	fieldDefs := make(map[string]FieldDefinition, len(s.Fields))
	for _, f := range s.Fields {
		fieldDefs[f.Name] = f
	}

	// Check for unknown fields.
	for name := range attrs {
		if _, ok := fieldDefs[name]; !ok {
			return fmt.Errorf("unknown field %q", name)
		}
	}

	// Check required fields and types.
	for _, f := range s.Fields {
		val, ok := attrs[f.Name]
		if !ok {
			if f.Required {
				return fmt.Errorf("required field %q is missing", f.Name)
			}
			continue
		}
		if err := validateFieldValue(f, val); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return nil
}

func validateFieldValue(f FieldDefinition, val any) error {
	switch f.Type {
	case FieldTypeString, FieldTypeReference:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
	case FieldTypeInt:
		switch val.(type) {
		case int, int64, float64:
			// float64 is accepted because JSON numbers decode as float64.
			// We'll coerce at storage time.
		default:
			return fmt.Errorf("expected int, got %T", val)
		}
	case FieldTypeFloat:
		switch val.(type) {
		case float64, float32, int, int64:
		default:
			return fmt.Errorf("expected float, got %T", val)
		}
	case FieldTypeBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", val)
		}
	case FieldTypeStringSlice:
		slice, ok := val.([]any)
		if !ok {
			return fmt.Errorf("expected []string, got %T", val)
		}
		for i, v := range slice {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("element [%d]: expected string, got %T", i, v)
			}
		}
	case FieldTypeIntSlice:
		slice, ok := val.([]any)
		if !ok {
			return fmt.Errorf("expected []int, got %T", val)
		}
		for i, v := range slice {
			switch v.(type) {
			case int, int64, float64:
			default:
				return fmt.Errorf("element [%d]: expected int, got %T", i, v)
			}
		}
	case FieldTypeFloatSlice:
		slice, ok := val.([]any)
		if !ok {
			return fmt.Errorf("expected []float, got %T", val)
		}
		for i, v := range slice {
			switch v.(type) {
			case float64, float32, int, int64:
			default:
				return fmt.Errorf("element [%d]: expected float, got %T", i, v)
			}
		}
	case FieldTypeDatetime:
		switch v := val.(type) {
		case string:
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				return fmt.Errorf("expected RFC3339 datetime string, got %q", v)
			}
		default:
			return fmt.Errorf("expected datetime string, got %T", val)
		}
	default:
		return fmt.Errorf("unknown field type %v", f.Type)
	}
	return nil
}
