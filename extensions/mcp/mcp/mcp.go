// Package mcp serves the API's action catalog as MCP tools over stdio: one narrow tool per action (small local
// models do better with fixed tools than with free browsing), plus get to follow links. Every call goes through the aminte API, and every result is the resource's Siren
// entity, links and actions included.
package mcp

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"aminte/client"
	"aminte/client/tools"
	"aminte/siren"
)

// Serve runs the MCP server on in and out, the stdin and the stdout of the process, until the client goes away. It
// closes in at the end and leaves out open, as the SDK's StdioTransport does with the stdio of the process.
func Serve(ctx context.Context, c *client.Client, version string, in io.ReadCloser, out io.Writer) error {
	s, err := New(ctx, c, version)
	if err != nil {
		return err
	}
	return Run(ctx, s, in, out)
}

// Run runs a built server on in and out until the client goes away or ctx ends.
func Run(ctx context.Context, s *mcp.Server, in io.ReadCloser, out io.Writer) error {
	return s.Run(ctx, &mcp.IOTransport{Reader: in, Writer: openWriter{out}})
}

// openWriter is a writer whose Close does nothing.
type openWriter struct{ io.Writer }

func (openWriter) Close() error { return nil }

// New builds the server from GET /actions.
func New(ctx context.Context, c *client.Client, version string) (*mcp.Server, error) {
	actions, err := c.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	list, err := tools.Of(actions)
	if err != nil {
		return nil, err
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "aminte", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	for _, t := range list {
		s.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: t.Schema, Annotations: annotations(t)}, call(c, t))
	}
	return s, nil
}

// annotations say what a tool does to the database: a tool that only reads (get and a GET of the catalog) says so; a catalog action marked Danger is destructive, as a browser draws it.
func annotations(t tools.Tool) *mcp.ToolAnnotations {
	if t.Action == nil || t.Action.Method == "GET" {
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	}
	danger := t.Action.Danger
	return &mcp.ToolAnnotations{DestructiveHint: &danger}
}

func call(c *client.Client, t tools.Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := tools.Arguments(req.Params.Arguments)
		if err != nil {
			return failure(err), nil
		}
		return result(tools.Run(ctx, c, t, args))
	}
}

func result(e *siren.Entity, err error) (*mcp.CallToolResult, error) {
	if e == nil && err != nil {
		return failure(err), nil
	}
	var b bytes.Buffer
	if err := siren.WriteJSON(&b, e, ""); err != nil {
		return failure(err), nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSuffix(b.String(), "\n")}}, IsError: err != nil}, nil
}

func failure(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}
}
