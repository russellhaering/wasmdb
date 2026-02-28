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
	{
		Name: "_sessions",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "token_hash", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "user_id", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "user_email", Type: document.FieldTypeString, Required: true},
				{Name: "expires_at", Type: document.FieldTypeDatetime, Required: true},
			},
		},
	},
	{
		Name: "_functions",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "description", Type: document.FieldTypeString},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
	{
		Name: "_chat_sessions",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "user_id", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "title", Type: document.FieldTypeString},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
	{
		Name: "_skills",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "description", Type: document.FieldTypeString},
				{Name: "function_name", Type: document.FieldTypeString, Indexed: true},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
}
