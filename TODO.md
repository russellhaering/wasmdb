# TODO

## Data UI Builder
Allow creation of a UI for data stored in the database. UI configuration is stored in a system table. Use Google's A2UI framework (research in depth at implementation time). Pre-create an agent that runs daily and updates the UI based on the schema and a sample of data in the system.

## Rego-Based Permissions
Add a permission system built on [OPA/Rego](https://www.openpolicyagent.org/). Policies would govern who can read/write which tables, documents, or fields. Evaluate policies per-request using the bearer token identity as input.

## Agent Data Display (Tables & Cards)
Introduce a way for agents to render structured data in conversations — both tabular views and some kind of "card" format for individual records. This would let AI chat responses include rich, formatted data displays rather than raw JSON.

## Stored Functions
Introduce a concept of "functions" — the ability to store code in the database, then execute that code on demand. Stored functions must themselves be able to operate on the database (read/write documents, query indexes, etc.). This is the foundation for server-side logic, triggers, and computed fields.

## Agents, Skills & Memories
Introduce first-class concepts of "agents", "skills", and "memories", all stored in the database (likely as system tables). Agents are configurable AI actors; skills define reusable capabilities an agent can invoke; memories are persistent context that agents accumulate over time and can recall in future interactions.

## Agent MCP Server Configuration
Allow MCP servers to be configured per-agent via a system table. This lets each agent have its own set of external tool integrations (e.g. Slack, GitHub, databases) without hardcoding them in the server config.

## Chat Agent Activity Indicator ✅
Implemented real SSE streaming (replaced fake batch-then-emit with `anthropic.NewStreaming()` + `Accumulate()`). Text deltas now stream token-by-token to the browser. Tool call indicators have animated dots (`...` CSS animation) while pending, switching to "done"/"error" on completion. Combined with token-level streaming, the UI no longer feels stuck during agent turns.

## Chat Session Persistence ✅
Chat sessions are now persisted to the `_chat_sessions` system table. Message history is JSON-serialized in the document Content field, keyed by ULID session IDs. Sessions survive restarts with an LRU in-memory cache (100 sessions) and DB fallback. The UI has a sidebar for session management (list, switch, delete, new).
