package graphqlapi

import (
	"context"
	"fmt"

	"github.com/graphql-go/graphql"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
)

// buildSchema constructs a graphql.Schema from the current state of the registry.
// Each database gets its own document type with typed attribute fields, plus
// query fields for get, text search, vector search, and attribute search.
func buildSchema(ctx context.Context, registry *database.Registry) (graphql.Schema, error) {
	queryFields := graphql.Fields{
		"databases": &graphql.Field{
			Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(databaseInfoType))),
			Resolve: resolveListDatabases(registry),
		},
		"database": &graphql.Field{
			Type: databaseInfoType,
			Args: graphql.FieldConfigArgument{
				"name": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
			},
			Resolve: resolveGetDatabase(registry),
		},
	}

	metas, err := registry.ListDatabases(ctx)
	if err != nil {
		return graphql.Schema{}, fmt.Errorf("list databases: %w", err)
	}

	for _, meta := range metas {
		docType := buildDocumentType(meta.Name, meta.Schema)
		addDatabaseQueryFields(queryFields, registry, meta.Name, meta.Schema, docType)
	}

	schemaConfig := graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: queryFields,
		}),
	}

	return graphql.NewSchema(schemaConfig)
}

// buildDocumentType creates a GraphQL object type for documents in a specific database.
// It includes base document fields plus typed fields for each schema attribute.
func buildDocumentType(dbName string, schema *document.Schema) *graphql.Object {
	fields := baseDocumentFields()

	if schema != nil {
		for _, fd := range schema.Fields {
			gqlType := graphqlFieldType(fd.Type)
			fieldName := "attr_" + fd.Name
			fields[fieldName] = &graphql.Field{
				Type:        gqlType,
				Description: fmt.Sprintf("Attribute: %s (%s)", fd.Name, fd.Type),
			}
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   dbName + "_Document",
		Fields: fields,
	})
}

// textSearchResultType creates a per-database text search result type wrapping documents + total.
func textSearchResultType(dbName string, docType *graphql.Object) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: dbName + "_TextSearchResult",
		Fields: graphql.Fields{
			"documents": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(docType)))},
			"total":     &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
		},
	})
}

// addDatabaseQueryFields adds get/search query fields for a database.
func addDatabaseQueryFields(
	fields graphql.Fields,
	registry *database.Registry,
	dbName string,
	schema *document.Schema,
	docType *graphql.Object,
) {
	// Get document by ID.
	fields["get_"+dbName] = &graphql.Field{
		Type:        docType,
		Description: fmt.Sprintf("Get a document by ID from the %s database", dbName),
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
		},
		Resolve: resolveGetDocument(registry, dbName),
	}

	// Full-text search.
	fields["search_"+dbName+"_text"] = &graphql.Field{
		Type:        textSearchResultType(dbName, docType),
		Description: fmt.Sprintf("Full-text search in the %s database", dbName),
		Args: graphql.FieldConfigArgument{
			"query":  &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
			"limit":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 10},
			"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
		},
		Resolve: resolveSearchText(registry, dbName),
	}

	// Vector search (by text query).
	if schema != nil && schema.EmbeddingDimensions > 0 {
		fields["search_"+dbName+"_vector"] = &graphql.Field{
			Type:        graphql.NewList(graphql.NewNonNull(docType)),
			Description: fmt.Sprintf("Vector similarity search in the %s database", dbName),
			Args: graphql.FieldConfigArgument{
				"query": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
				"k":     &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 10},
			},
			Resolve: resolveSearchVector(registry, dbName),
		}
	}

	// Attribute search.
	fields["search_"+dbName+"_attributes"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(docType))),
		Description: fmt.Sprintf("Attribute filter search in the %s database", dbName),
		Args: graphql.FieldConfigArgument{
			"filters": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(filterInput)))},
			"limit":   &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 10},
			"offset":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
		},
		Resolve: resolveSearchAttributes(registry, dbName),
	}
}
