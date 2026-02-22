package document

import (
	"testing"
)

func TestSchemaValidate(t *testing.T) {
	schema := &Schema{
		Fields: []FieldDefinition{
			{Name: "title", Type: FieldTypeString, Required: true},
			{Name: "count", Type: FieldTypeInt},
			{Name: "score", Type: FieldTypeFloat},
			{Name: "active", Type: FieldTypeBool},
			{Name: "tags", Type: FieldTypeStringSlice},
			{Name: "date", Type: FieldTypeDatetime},
			{Name: "ref", Type: FieldTypeReference},
		},
	}

	tests := []struct {
		name    string
		attrs   map[string]any
		wantErr bool
	}{
		{
			name:    "valid full",
			attrs:   map[string]any{"title": "Hello", "count": float64(5), "active": true},
			wantErr: false,
		},
		{
			name:    "missing required",
			attrs:   map[string]any{"count": float64(1)},
			wantErr: true,
		},
		{
			name:    "unknown field",
			attrs:   map[string]any{"title": "Hello", "unknown": "x"},
			wantErr: true,
		},
		{
			name:    "wrong type",
			attrs:   map[string]any{"title": 123},
			wantErr: true,
		},
		{
			name:    "valid datetime",
			attrs:   map[string]any{"title": "Hi", "date": "2024-01-15T10:30:00Z"},
			wantErr: false,
		},
		{
			name:    "invalid datetime",
			attrs:   map[string]any{"title": "Hi", "date": "not-a-date"},
			wantErr: true,
		},
		{
			name:    "valid string slice",
			attrs:   map[string]any{"title": "Hi", "tags": []any{"a", "b"}},
			wantErr: false,
		},
		{
			name:    "invalid string slice element",
			attrs:   map[string]any{"title": "Hi", "tags": []any{"a", 1}},
			wantErr: true,
		},
		{
			name:    "valid reference",
			attrs:   map[string]any{"title": "Hi", "ref": "doc-abc"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.Validate(tt.attrs)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldTypeMarshalRoundTrip(t *testing.T) {
	types := []FieldType{
		FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool,
		FieldTypeStringSlice, FieldTypeIntSlice, FieldTypeFloatSlice,
		FieldTypeDatetime, FieldTypeReference,
	}
	for _, ft := range types {
		data, err := ft.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", ft, err)
		}
		var got FieldType
		if err := got.UnmarshalText(data); err != nil {
			t.Fatalf("UnmarshalText(%s): %v", data, err)
		}
		if got != ft {
			t.Errorf("round-trip: got %v, want %v", got, ft)
		}
	}
}
