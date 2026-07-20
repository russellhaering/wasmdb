# Surface UI Format

A page is a flat list of components plus a set of declared actions. Each component has a unique `id`, a `type`, and `properties`. Exactly one component must have the id `root`; it is the entry point. Layout components (`Column`, `Row`, `Card`) list the ids of their `children`; all other components are leaves and must not have children. Unknown types and unknown properties are rejected.

A page is JSON of the form:

```json
{
  "components": [
    {"id": "root", "type": "Column", "children": ["t1"]},
    {"id": "t1", "type": "Text", "properties": {"value": "Hello"}}
  ]
}
```

## Components

### Column

Vertical layout container. Stacks its children top to bottom. May have children.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `gap` | int | no | no | Spacing between children (0-256). |
| `align` | enum(start|center|end) | no | no | Cross-axis alignment of children. |

### Row

Horizontal layout container. Places its children left to right. May have children.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `gap` | int | no | no | Spacing between children (0-256). |
| `align` | enum(start|center|end) | no | no | Cross-axis alignment of children. |

### Card

Titled panel that groups its children. May have children.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `title` | string | no | yes | Optional panel heading. |

### Divider

Horizontal separator. Has no properties and no children.

### Text

A run of text.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `value` | string | yes | yes | The text to display. |
| `variant` | enum(body|heading|caption|code) | no | no | Visual style. |

### Metric

A single labeled statistic (stat tile).

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `label` | string | yes | no | What the value measures. |
| `value` | string|number | yes | yes | The statistic itself. |
| `unit` | string | no | no | Optional unit suffix. |

### DataTable

Tabular display of rows, optionally bound to query data.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `columns` | array of {key:string, label:string, type?:string} | yes | no | Column definitions. type is one of string|int|float|bool|datetime. |
| `rows` | array of objects OR a $data ref resolving to an array | yes | yes | Row data. Usually a $data ref into query data. |
| `empty_text` | string | no | no | Text shown when there are no rows. |
| `row_actions` | array of {action:string, label:string, confirm?:bool} | no | no | Per-row buttons. action must name an update/delete/query action. |

### Form

Group of input fields with a submit button.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `fields` | array of {name, label, type, required?, options?, default?} | yes | no | Field definitions. type is string|int|float|bool|datetime|select; options is required when type is select. |
| `submit` | {action:string, label:string} | yes | no | Submit button. action must name an insert/update/query action. |

### Input

Standalone control, e.g. a search box. Bound inputs feed render params.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `name` | string | yes | no | Parameter name this input feeds. |
| `type` | enum(string|int|float|bool|datetime|select) | yes | no | Value type. |
| `label` | string | no | no | Field label. |
| `placeholder` | string | no | no | Placeholder text. |
| `options` | array of strings | no | no | Choices; required when type is select, otherwise omitted. |
| `bind` | bool | no | no | Whether the value feeds render params. Defaults to true. |

### Button

Triggers a declared action when clicked.

| Property | Type | Required | $data | Description |
|---|---|---|---|---|
| `label` | string | yes | no | Button text. |
| `action` | string (declared action name) | yes | no | The action to run. |
| `params` | object; values may be $data refs | no | no | Static or bound parameters passed to the action. |
| `confirm` | bool | no | no | Prompt for confirmation before running. |

## Data binding

Any property marked "$data: yes" may hold a reference of the form `{"$data": "path.to.value"}` instead of a literal value. The path is resolved (dot-separated) against the data object returned by the page's query at render time. There is no string templating. Example:

```json
{"id": "tbl", "type": "DataTable", "properties": {
  "columns": [{"key": "name", "label": "Name"}],
  "rows": {"$data": "orders"}
}}
```

A `DataTable`'s `rows` $data path must resolve to an array. $data is not allowed inside `columns`, `fields`, or action names.

## Actions

Actions are declared once per page, separately from components, and referenced by `id`. Types:

- `insert` / `update` / `delete`: write to `table` (required; system tables, names starting with `_`, are rejected).
- `query`: re-run the page's query with the given `params` and return fresh data (this is how search, filter, and pagination work).

```json
{
  "create_order": {"type": "insert", "table": "orders"},
  "delete_order": {"type": "delete", "table": "orders", "confirm": true},
  "search":       {"type": "query", "params": ["q"]}
}
```

A `Button` and `Form` submit may reference `insert`/`update`/`query` actions; `DataTable` row actions may reference `update`/`delete`/`query` (never `insert`).

## Worked example

A page listing orders with a search box, a create form, and a refresh button:

```json
{
  "components": [
    {"id": "root", "type": "Column", "properties": {"gap": 16}, "children": ["hdr", "search", "tbl", "create", "refresh"]},
    {"id": "hdr", "type": "Text", "properties": {"value": "Orders", "variant": "heading"}},
    {"id": "search", "type": "Input", "properties": {"name": "q", "type": "string", "label": "Search", "bind": true}},
    {"id": "tbl", "type": "DataTable", "properties": {
      "columns": [
        {"key": "id", "label": "ID"},
        {"key": "total", "label": "Total", "type": "float"}
      ],
      "rows": {"$data": "orders"},
      "empty_text": "No orders yet.",
      "row_actions": [{"action": "delete_order", "label": "Delete", "confirm": true}]
    }},
    {"id": "create", "type": "Form", "properties": {
      "fields": [
        {"name": "customer", "label": "Customer", "type": "string", "required": true},
        {"name": "total", "label": "Total", "type": "float"}
      ],
      "submit": {"action": "create_order", "label": "Create order"}
    }},
    {"id": "refresh", "type": "Button", "properties": {"label": "Refresh", "action": "search", "params": {"q": {"$data": "query.q"}}}}
  ],
  "actions": {
    "create_order": {"type": "insert", "table": "orders"},
    "delete_order": {"type": "delete", "table": "orders", "confirm": true},
    "search": {"type": "query", "params": ["q"]}
  }
}
```
