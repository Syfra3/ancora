# Ancora Implementation Tasks for Vela Forwarding

## Goal

Keep Ancora as the canonical memory MCP while optionally exposing Vela-powered graph retrieval tools when Vela is installed.

## Tasks

### 1. Detect Vela availability

- Add a small integration check in setup/startup code.
- Detect whether Vela is installed and reachable as a local MCP-capable command.
- Use that to distinguish `ancora only` from `ancora + vela` mode.

## 2. Add optional forwarded `vela_*` tools

- In combined mode, expose these tools from the primary Ancora MCP surface:
  - `vela_query_graph`
  - `vela_shortest_path`
  - `vela_get_node`
  - `vela_get_neighbors`
  - `vela_graph_stats`
  - `vela_explain_graph`
  - `vela_federated_search`
- Keep their names as `vela_*` so ownership stays visible.

## 3. Implement forwarding/proxy layer

- Add a Vela client or adapter inside Ancora MCP handling.
- Forward approved `vela_*` requests to the local Vela MCP process.
- Return Vela responses without mixing them into `ancora_*` semantics.

## 4. Protect canonical memory ownership

- Ensure only `ancora_*` tools can save, update, summarize, or recall canonical memory.
- Add guardrails so no forwarded Vela tool can mutate durable memory.

## 5. Make setup/install modes explicit

- `ancora only`: register only `ancora_*`
- `ancora + vela`: register `ancora_*` plus forwarded `vela_*`
- Document the visible tool surface in setup output and docs.

## 6. Add contract tests

- Test tool visibility in `ancora only` and `ancora + vela` modes.
- Test forwarding success and failure paths.
- Test that duplicate memory tools never appear.

## Primary Files

- `internal/mcp/mcp.go`
- `internal/setup/setup.go`
- `internal/setup/`
- `README.md`
- `plugin/claude-code/skills/memory/SKILL.md`

## Done Criteria

- Ancora stays the only canonical memory surface.
- Combined mode exposes approved `vela_*` retrieval tools.
- Forwarding is optional and safe when Vela is missing.
- Tool names and behavior match the integration contract.
