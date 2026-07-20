package database

import "github.com/russellhaering/moraine/document"

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
				{Name: "disable_model_invocation", Type: document.FieldTypeBool},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
	{
		Name: "_mcp_servers",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "description", Type: document.FieldTypeString},
				{Name: "transport", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "url", Type: document.FieldTypeString},
				{Name: "command", Type: document.FieldTypeString},
				{Name: "enabled", Type: document.FieldTypeBool, Indexed: true},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
	{
		Name: "_agents",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "description", Type: document.FieldTypeString},
				{Name: "prompt", Type: document.FieldTypeString, Required: true},
				{Name: "schedule", Type: document.FieldTypeString, Required: true},
				{Name: "trigger_type", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "enabled", Type: document.FieldTypeBool, Indexed: true},
				{Name: "max_turns", Type: document.FieldTypeInt},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
	{
		Name: "_agent_runs",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "agent_id", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "agent_name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "status", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "output", Type: document.FieldTypeString},
				{Name: "error", Type: document.FieldTypeString},
				{Name: "input_tokens", Type: document.FieldTypeInt},
				{Name: "output_tokens", Type: document.FieldTypeInt},
				{Name: "duration_ms", Type: document.FieldTypeInt},
				{Name: "started_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
				{Name: "completed_at", Type: document.FieldTypeDatetime, Indexed: true},
			},
		},
	},
	{
		Name: "_memories",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "user_id", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "scope", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "title", Type: document.FieldTypeString, Indexed: true},
				{Name: "summary", Type: document.FieldTypeString},
				{Name: "tags", Type: document.FieldTypeStringSlice, Indexed: true},
				{Name: "pinned", Type: document.FieldTypeBool, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
				{Name: "last_used_at", Type: document.FieldTypeDatetime, Indexed: true},
			},
		},
	},
	{
		Name: "_ui_configs",
		Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "title", Type: document.FieldTypeString},
				{Name: "description", Type: document.FieldTypeString},
				{Name: "source_tables", Type: document.FieldTypeStringSlice, Indexed: true},
				{Name: "surface_json", Type: document.FieldTypeString, Required: true},
				{Name: "actions_json", Type: document.FieldTypeString},
				{Name: "query_js", Type: document.FieldTypeString},
				{Name: "auto_refresh_seconds", Type: document.FieldTypeInt},
				{Name: "sort_order", Type: document.FieldTypeInt, Indexed: true},
				{Name: "enabled", Type: document.FieldTypeBool, Indexed: true},
				{Name: "spec_version", Type: document.FieldTypeInt, Indexed: true},
				{Name: "generator", Type: document.FieldTypeString, Indexed: true},
				{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
				{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
			},
		},
	},
}
