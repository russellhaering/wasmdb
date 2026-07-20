package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/russellhaering/moraine/index"
)

func init() {
	register(command{
		noun:        "search",
		verb:        "text",
		usage:       "wasmdb search text <db> <query> [--limit N] [--offset N] [--json]",
		description: "Full-text search",
		run:         searchText,
	})
	register(command{
		noun:        "search",
		verb:        "vector",
		usage:       "wasmdb search vector <db> --query <text> [--k N] [--json]",
		description: "Vector similarity search",
		run:         searchVector,
	})
	register(command{
		noun:        "search",
		verb:        "attr",
		usage:       "wasmdb search attr <db> [--filter field=op:value]... [--limit N] [--offset N] [--json]",
		description: "Attribute filter search",
		run:         searchAttr,
	})
}

func searchText(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return fmt.Errorf("usage: wasmdb search text <db> <query>")
	}
	db, query := ctx.args[0], ctx.args[1]

	limit, _ := strconv.Atoi(ctx.flag("limit"))
	offset, _ := strconv.Atoi(ctx.flag("offset"))
	if limit <= 0 {
		limit = 10
	}

	result, err := ctx.backend.SearchText(ctx, db, query, limit, offset)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	formatTextSearchResult(ctx.stdout, result)
	return nil
}

func searchVector(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb search vector <db> --query <text>")
	}
	db := ctx.args[0]

	query := ctx.flag("query")
	if query == "" {
		return fmt.Errorf("--query is required")
	}

	k, _ := strconv.Atoi(ctx.flag("k"))
	if k <= 0 {
		k = 10
	}

	docs, err := ctx.backend.SearchVector(ctx, db, query, k)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, docs)
	}
	formatDocumentList(ctx.stdout, docs)
	return nil
}

func searchAttr(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb search attr <db> [--filter field=op:value]...")
	}
	db := ctx.args[0]

	filterStrs := ctx.flagAll("filter")
	filters, err := parseFilters(filterStrs)
	if err != nil {
		return err
	}

	limit, _ := strconv.Atoi(ctx.flag("limit"))
	offset, _ := strconv.Atoi(ctx.flag("offset"))
	if limit <= 0 {
		limit = 10
	}

	docs, err := ctx.backend.SearchAttributes(ctx, db, filters, limit, offset)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, docs)
	}
	formatDocumentList(ctx.stdout, docs)
	return nil
}

// parseFilters parses --filter field=op:value strings into index.Filter slices.
func parseFilters(strs []string) ([]index.Filter, error) {
	filters := make([]index.Filter, 0, len(strs))
	for _, s := range strs {
		field, rest, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("invalid filter: %q (expected field=op:value)", s)
		}
		op, value, ok := strings.Cut(rest, ":")
		if !ok {
			return nil, fmt.Errorf("invalid filter: %q (expected field=op:value)", s)
		}
		filters = append(filters, index.Filter{
			Field: field,
			Op:    index.FilterOp(op),
			Value: parseJSONValue(value),
		})
	}
	return filters, nil
}

