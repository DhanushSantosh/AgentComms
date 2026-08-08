package mcp

// ToolDocumentation returns the canonical MCP tool descriptors used by the
// live tools/list response. Each call receives a newly allocated catalog.
func ToolDocumentation() []map[string]any {
	return tools()
}
