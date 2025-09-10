package mcp

import (
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// NewRemoteClient creates a new MCP client that can connect to a remote MCP server
func NewRemoteClient(url, transportType string, headers map[string]string) (*Client, error) {
	slog.Debug("Creating remote MCP client", "url", url, "transport", transportType, "headers", headers)

	var c *client.Client
	if transportType == "sse" {
		var err error
		c, err = client.NewSSEMCPClient(url, client.WithHeaders(headers))
		if err != nil {
			slog.Error("Failed to create sse remote MCP client", "error", err)
			return nil, fmt.Errorf("failed to create sse remote MCP client: %w", err)
		}
	} else {
		var err error
		c, err = client.NewStreamableHttpClient(url, transport.WithHTTPHeaders(headers))
		if err != nil {
			slog.Error("Failed to create streamable remote MCP client", "error", err)
			return nil, fmt.Errorf("failed to create streamable remote MCP client: %w", err)
		}
	}

	slog.Debug("Created remote MCP client successfully")
	return &Client{
		client:    c,
		logType:   "remote",
		logId:     url,
		serverURL: url,
	}, nil
}
