package graphqlapi

import (
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/russellhaering/moraine/document"
)

// fieldTypeEnum maps document.FieldType values to a GraphQL enum.
var fieldTypeEnum = graphql.NewEnum(graphql.EnumConfig{
	Name: "FieldType",
	Values: graphql.EnumValueConfigMap{
		"STRING":       {Value: document.FieldTypeString},
		"INT":          {Value: document.FieldTypeInt},
		"FLOAT":        {Value: document.FieldTypeFloat},
		"BOOL":         {Value: document.FieldTypeBool},
		"STRING_SLICE": {Value: document.FieldTypeStringSlice},
		"INT_SLICE":    {Value: document.FieldTypeIntSlice},
		"FLOAT_SLICE":  {Value: document.FieldTypeFloatSlice},
		"DATETIME":     {Value: document.FieldTypeDatetime},
		"REFERENCE":    {Value: document.FieldTypeReference},
	},
})

var fieldDefinitionType = graphql.NewObject(graphql.ObjectConfig{
	Name: "FieldDefinition",
	Fields: graphql.Fields{
		"name":        &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"type":        &graphql.Field{Type: graphql.NewNonNull(fieldTypeEnum)},
		"required":    &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
		"indexed":     &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
		"fullText":    &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
		"referenceDb": &graphql.Field{Type: graphql.String},
	},
})

var schemaType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Schema",
	Fields: graphql.Fields{
		"fields":              &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(fieldDefinitionType)))},
		"embeddingModel":      &graphql.Field{Type: graphql.String},
		"embeddingDimensions": &graphql.Field{Type: graphql.Int},
	},
})

var tableInfoType = graphql.NewObject(graphql.ObjectConfig{
	Name: "TableInfo",
	Fields: graphql.Fields{
		"name":      &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"schema":    &graphql.Field{Type: schemaType},
		"system":    &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
		"createdAt": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
	},
})

var filterOpEnum = graphql.NewEnum(graphql.EnumConfig{
	Name: "FilterOp",
	Values: graphql.EnumValueConfigMap{
		"EQ":       {Value: "eq"},
		"NEQ":      {Value: "neq"},
		"GT":       {Value: "gt"},
		"GTE":      {Value: "gte"},
		"LT":       {Value: "lt"},
		"LTE":      {Value: "lte"},
		"IN":       {Value: "in"},
		"CONTAINS": {Value: "contains"},
	},
})

var filterInput = graphql.NewInputObject(graphql.InputObjectConfig{
	Name: "FilterInput",
	Fields: graphql.InputObjectConfigFieldMap{
		"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
		"op":    &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(filterOpEnum)},
		"value": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
	},
})

// jsonScalar is a scalar that passes through arbitrary JSON values.
var jsonScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSON",
	Description: "Arbitrary JSON value",
	Serialize:   func(value any) any { return value },
	ParseValue:  func(value any) any { return value },
	ParseLiteral: func(valueAST ast.Value) interface{} {
		return nil
	},
})

// baseDocumentFields returns the common document fields shared across all tables.
// The returned map can be extended with per-table attribute fields.
func baseDocumentFields() graphql.Fields {
	return graphql.Fields{
		"id":         &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		"content":    &graphql.Field{Type: graphql.String},
		"attributes": &graphql.Field{Type: jsonScalar},
		"embedding":  &graphql.Field{Type: graphql.NewList(graphql.Float)},
		"createdAt": &graphql.Field{
			Type: graphql.NewNonNull(graphql.String),
		},
		"updatedAt": &graphql.Field{
			Type: graphql.NewNonNull(graphql.String),
		},
		"version": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
	}
}

// graphqlFieldType maps a document.FieldType to the corresponding graphql.Output type.
func graphqlFieldType(ft document.FieldType) graphql.Output {
	switch ft {
	case document.FieldTypeString, document.FieldTypeDatetime, document.FieldTypeReference:
		return graphql.String
	case document.FieldTypeInt:
		return graphql.Int
	case document.FieldTypeFloat:
		return graphql.Float
	case document.FieldTypeBool:
		return graphql.Boolean
	case document.FieldTypeStringSlice:
		return graphql.NewList(graphql.String)
	case document.FieldTypeIntSlice:
		return graphql.NewList(graphql.Int)
	case document.FieldTypeFloatSlice:
		return graphql.NewList(graphql.Float)
	default:
		return graphql.String
	}
}
