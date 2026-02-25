package database

import "github.com/russellhaering/wasmdb/internal/document"

// SystemTables defines the system tables to be auto-created at startup.
var SystemTables = []SystemTableDef{
	{
		Name: "_users",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "email", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "password_hash", Type: document.FieldTypeString, Required: true},
			},
		},
	},
}
