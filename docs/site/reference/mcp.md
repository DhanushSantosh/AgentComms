---
title: MCP tool reference
description: Inspect the canonical tool descriptions, required arguments, bounds, and enums advertised through tools/list.
section: Reference
order: 2
audience: Agents
template: mcp-reference
lastVerified: 2026-08-01
related: [agents/mcp, agents/invocations]
---

This catalog is generated from the descriptors returned by the live MCP server. Tool calls use the actor identity bound to the MCP connection and the same authority rules as CLI commands.

An empty `required` array is emitted for tools without mandatory arguments so strict MCP hosts receive valid JSON Schema.
