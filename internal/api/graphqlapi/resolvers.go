package graphqlapi

import (
	"fmt"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/moraine/document"
	"github.com/russellhaering/moraine/index"
)

// documentToMap converts a document.Document to a map for GraphQL resolution.
func documentToMap(doc *document.Document) map[string]any {
	m := map[string]any{
		"id":         doc.ID,
		"content":    doc.Content,
		"attributes": doc.Attributes,
		"createdAt":  doc.CreatedAt.Format(time.RFC3339),
		"updatedAt":  doc.UpdatedAt.Format(time.RFC3339),
		"version":    doc.Version,
	}
	if len(doc.Embedding) > 0 {
		m["embedding"] = doc.Embedding
	}
	// Flatten attributes to top-level for typed field access.
	for k, v := range doc.Attributes {
		m["attr_"+k] = v
	}
	return m
}

// tableMetaToMap converts a TableMeta to a map for GraphQL resolution.
func tableMetaToMap(meta database.TableMeta) map[string]any {
	m := map[string]any{
		"name":      meta.Name,
		"system":    meta.System,
		"createdAt": meta.CreatedAt.Format(time.RFC3339),
	}
	if meta.Schema != nil {
		m["schema"] = schemaToMap(meta.Schema)
	}
	return m
}

func schemaToMap(s *document.Schema) map[string]any {
	fields := make([]map[string]any, len(s.Fields))
	for i, f := range s.Fields {
		fields[i] = map[string]any{
			"name":        f.Name,
			"type":        f.Type,
			"required":    f.Required,
			"indexed":     f.Indexed,
			"fullText":    f.FullText,
			"referenceDb": f.ReferenceDB,
		}
	}
	return map[string]any{
		"fields":              fields,
		"embeddingModel":      s.EmbeddingModel,
		"embeddingDimensions": s.EmbeddingDimensions,
	}
}

func resolveListTables(registry *database.Registry) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		metas, err := registry.ListTables(p.Context)
		if err != nil {
			return nil, err
		}
		result := make([]map[string]any, len(metas))
		for i, m := range metas {
			result[i] = tableMetaToMap(m)
		}
		return result, nil
	}
}

func resolveGetTable(registry *database.Registry) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		name, _ := p.Args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}

		metas, err := registry.ListTables(p.Context)
		if err != nil {
			return nil, err
		}
		for _, m := range metas {
			if m.Name == name {
				return tableMetaToMap(m), nil
			}
		}
		return nil, nil
	}
}

func resolveGetDocument(registry *database.Registry, dbName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		id, _ := p.Args["id"].(string)
		if id == "" {
			return nil, fmt.Errorf("id is required")
		}

		db, err := registry.GetTable(p.Context, dbName)
		if err != nil {
			return nil, err
		}

		doc, err := db.GetDocument(p.Context, id)
		if err != nil {
			return nil, err
		}
		if doc == nil {
			return nil, nil
		}
		return documentToMap(doc), nil
	}
}

func resolveSearchText(registry *database.Registry, dbName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		query, _ := p.Args["query"].(string)
		limit, _ := p.Args["limit"].(int)
		offset, _ := p.Args["offset"].(int)

		if limit <= 0 {
			limit = 10
		}

		db, err := registry.GetTable(p.Context, dbName)
		if err != nil {
			return nil, err
		}

		docs, total, err := db.SearchText(p.Context, query, limit, offset)
		if err != nil {
			return nil, err
		}

		docMaps := make([]map[string]any, len(docs))
		for i, d := range docs {
			docMaps[i] = documentToMap(d)
		}
		return map[string]any{
			"documents": docMaps,
			"total":     total,
		}, nil
	}
}

func resolveSearchVector(registry *database.Registry, dbName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		query, _ := p.Args["query"].(string)
		k, _ := p.Args["k"].(int)

		if k <= 0 {
			k = 10
		}

		db, err := registry.GetTable(p.Context, dbName)
		if err != nil {
			return nil, err
		}

		docs, err := db.SearchVectorByText(p.Context, query, k)
		if err != nil {
			return nil, err
		}

		result := make([]map[string]any, len(docs))
		for i, d := range docs {
			result[i] = documentToMap(d)
		}
		return result, nil
	}
}

func resolveSearchAttributes(registry *database.Registry, dbName string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		limit, _ := p.Args["limit"].(int)
		offset, _ := p.Args["offset"].(int)

		if limit <= 0 {
			limit = 10
		}

		// Parse filters from args.
		rawFilters, _ := p.Args["filters"].([]any)
		filters := make([]index.Filter, 0, len(rawFilters))
		for _, rf := range rawFilters {
			fm, ok := rf.(map[string]any)
			if !ok {
				continue
			}
			field, _ := fm["field"].(string)
			op, _ := fm["op"].(string)
			value, _ := fm["value"].(string)
			filters = append(filters, index.Filter{
				Field: field,
				Op:    index.FilterOp(op),
				Value: value,
			})
		}

		db, err := registry.GetTable(p.Context, dbName)
		if err != nil {
			return nil, err
		}

		docs, err := db.SearchAttributes(p.Context, filters, limit, offset)
		if err != nil {
			return nil, err
		}

		result := make([]map[string]any, len(docs))
		for i, d := range docs {
			result[i] = documentToMap(d)
		}
		return result, nil
	}
}
