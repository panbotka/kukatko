package mcpapi

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Every tool in this package carries annotations, and every one of them says
// openWorldHint: false. MCP defaults that hint to true, which tells a client the
// tool may reach an unpredictable external system; none of these do. They read
// and write this instance's own database and nothing else — the library is a
// closed world, and a client that knows it can stop treating each call as a trip
// outside.
//
// The other three hints follow the same rule of saying only what is true:
// readOnlyHint on the tools that change nothing, destructiveHint: false on the
// two that only add a new row (MCP defaults it to true, and a client that
// believes the default asks a human to confirm "create an empty album"), and
// idempotentHint only where calling twice really is the same as calling once.

// readAnnotations describes a tool that only reads the library.
func readAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
}

// additiveAnnotations describes a write tool that only creates something new and
// overwrites nothing, so a client need not warn about it. Calling it twice makes
// two records, so it is not idempotent.
func additiveAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(false)}
}

// idempotentAnnotations describes a write tool whose repetition changes nothing:
// the second identical call leaves the library exactly as the first did.
func idempotentAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(false)}
}

// writeAnnotations describes a write tool that may overwrite existing values and
// whose repetition is not free — the plain, unhinted write. Only bulk_edit_photos
// needs it: it removes albums and labels as readily as it adds them, and a
// repeated run applies its changes to whatever the photos look like by then.
func writeAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{OpenWorldHint: new(false)}
}
