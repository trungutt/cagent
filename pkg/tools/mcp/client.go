package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/docker/cagent/pkg/tools"
)

// ProtectedResourceMetadata represents the OAuth 2.0 Protected Resource Metadata
// as defined in RFC 8705 and MCP 2025-DRAFT-v2 specification
type ProtectedResourceMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

// ClientRegistrationRequest represents the OAuth 2.0 Dynamic Client Registration request
// as defined in RFC 7591
type ClientRegistrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
	TosURI                  string   `json:"tos_uri,omitempty"`
	PolicyURI               string   `json:"policy_uri,omitempty"`
	JwksURI                 string   `json:"jwks_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// ClientRegistrationResponse represents the OAuth 2.0 Dynamic Client Registration response
// as defined in RFC 7591
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	RegistrationAccessToken string   `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string   `json:"registration_client_uri,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
	TosURI                  string   `json:"tos_uri,omitempty"`
	PolicyURI               string   `json:"policy_uri,omitempty"`
	JwksURI                 string   `json:"jwks_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// PKCEParams holds PKCE parameters for OAuth 2.0 authorization
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	Method        string // S256
}

// AuthorizationURLParams holds all parameters needed to build a complete authorization URL
type AuthorizationURLParams struct {
	AuthorizationEndpoint string
	ResponseType          string
	ClientID              string
	RedirectURI           string
	CodeChallenge         string
	CodeChallengeMethod   string
	State                 string
	Scope                 string
}

// AuthorizationError is a special error type that carries authorization server information
// for 401 authentication errors
type AuthorizationError struct {
	Message             string
	AuthorizationServer string
	ServerURL           string
	AuthorizationURL    string
	ClientID            string
	RedirectURI         string
	CodeVerifier        string
	State               string
	OriginalError       error
}

func (e *AuthorizationError) Error() string {
	if e.AuthorizationServer != "" {
		return fmt.Sprintf("%s (authorization server: %s)", e.Message, e.AuthorizationServer)
	}
	return e.Message
}

func (e *AuthorizationError) Unwrap() error {
	return e.OriginalError
}

type mcpClient interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	ListPrompts(ctx context.Context, request mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error)
	ListResources(ctx context.Context, request mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error)
	ListResourceTemplates(ctx context.Context, request mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	GetPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
	ReadResource(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
	Close() error
}

// Client implements an MCP client for interacting with MCP servers
type Client struct {
	client    mcpClient
	tools     []tools.Tool
	logType   string
	logId     string
	serverURL string
}

// Start initializes and starts the MCP server connection
func (c *Client) Start(ctx context.Context) error {
	slog.Debug("Starting MCP client", c.logType, c.logId)

	if err := c.client.Start(ctx); err != nil {
		slog.Error("Failed to start MCP client", "error", err)

		// Handle authorization errors by returning special AuthorizationError
		if authErr := c.createAuthorizationError(ctx, err); authErr != nil {
			return authErr
		}

		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	slog.Debug("Initializing MCP client", c.logType, c.logId)
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "cagent",
		Version: "1.0.0",
	}

	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		_, err := c.client.Initialize(ctx, initRequest)
		if err == nil {
			break
		}
		// TODO(krissetto): This is a temporary fix to handle the case where the remote server hasn't finished its async init
		// and we send the notifications/initialized message before the server is ready. Fix upstream in mcp-go if possible.
		//
		// Only retry when initialization fails due to sending the initialized notification.
		if !isInitNotificationSendError(err) {
			slog.Error("Failed to initialize MCP client", "error", err)
			return fmt.Errorf("failed to initialize MCP client: %w", err)
		}
		if attempt >= maxRetries {
			slog.Error("Failed to initialize MCP client after retries", "error", err)
			return fmt.Errorf("failed to initialize MCP client after retries: %w", err)
		}
		backoff := time.Duration(200*(attempt+1)) * time.Millisecond
		slog.Debug("MCP initialize failed to send initialized notification; retrying", "id", c.logId, "attempt", attempt+1, "backoff_ms", backoff.Milliseconds())
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return fmt.Errorf("failed to initialize MCP client: %w", ctx.Err())
		}
	}

	slog.Debug("MCP client started and initialized successfully")
	return nil
}

// isInitNotificationSendError returns true if initialization failed while sending the
// notifications/initialized message to the server.
func isInitNotificationSendError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// mcp-go client returns this error
	if strings.Contains(msg, "failed to send initialized notification") {
		return true
	}
	return false
}

// Stop stops the MCP server
func (c *Client) Stop() error {
	slog.Debug("Stopping MCP client")
	err := c.client.Close()
	if err != nil {
		slog.Error("Failed to stop MCP client", "error", err)
		return err
	}
	slog.Debug("MCP client stopped successfully")
	return nil
}

// ListTools fetches available tools from the MCP server
func (c *Client) ListTools(ctx context.Context, toolFilter []string) ([]tools.Tool, error) {
	slog.Debug("Listing tools from MCP server", "toolFilter", toolFilter)

	resp, err := c.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("ListTools canceled by context")
			return nil, err
		}
		slog.Error("Failed to list tools from MCP server", "error", err)
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	slog.Debug("Received tools from MCP server", "count", len(resp.Tools))

	var toolsList []tools.Tool
	for i := range resp.Tools {
		t := &resp.Tools[i]
		// If toolFilter is not empty, only include tools that are in the filter
		if len(toolFilter) > 0 && !slices.Contains(toolFilter, t.Name) {
			slog.Debug("Filtering out tool", "tool", t.Name)
			continue
		}

		tool := tools.Tool{
			Handler: c.CallTool,
			Function: &tools.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters: tools.FunctionParamaters{
					Type:       t.InputSchema.Type,
					Properties: t.InputSchema.Properties,
					Required:   t.InputSchema.Required,
				},
				Annotations: tools.ToolAnnotation{
					Title:           t.Annotations.Title,
					ReadOnlyHint:    t.Annotations.ReadOnlyHint,
					DestructiveHint: t.Annotations.DestructiveHint,
					IdempotentHint:  t.Annotations.IdempotentHint,
					OpenWorldHint:   t.Annotations.OpenWorldHint,
				},
			},
		}
		toolsList = append(toolsList, tool)

		slog.Debug("Added MCP tool", "tool", t.Name)
	}

	c.tools = toolsList
	slog.Debug("Finished listing MCP tools", "filtered_count", len(toolsList))
	return toolsList, nil
}

// CallTool calls a tool on the MCP server
func (c *Client) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	slog.Debug("Calling MCP tool", "tool", toolCall.Function.Name, "arguments", toolCall.Function.Arguments)

	if toolCall.Function.Arguments == "" {
		toolCall.Function.Arguments = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		slog.Error("Failed to parse tool arguments", "tool", toolCall.Function.Name, "error", err)
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = toolCall.Function.Name
	request.Params.Arguments = args

	resp, err := c.client.CallTool(ctx, request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			slog.Debug("CallTool canceled by context", "tool", toolCall.Function.Name)
			return nil, err
		}
		slog.Error("Failed to call MCP tool", "tool", toolCall.Function.Name, "error", err)
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}

	result := processMCPContent(resp)
	slog.Debug("MCP tool call completed", "tool", toolCall.Function.Name, "output_length", len(result.Output))
	slog.Debug(result.Output)
	return result, nil
}

func processMCPContent(toolResult *mcp.CallToolResult) *tools.ToolCallResult {
	finalContent := ""
	for _, resultContent := range toolResult.Content {
		if textContent, ok := resultContent.(mcp.TextContent); ok {
			finalContent += textContent.Text
		}
	}

	return &tools.ToolCallResult{
		Output: finalContent,
	}
}

// GetToolByName returns a tool by name
func (c *Client) GetToolByName(name string) (tools.Tool, error) {
	for _, tool := range c.tools {
		if tool.Function != nil && tool.Function.Name == name {
			return tool, nil
		}
	}
	return tools.Tool{}, fmt.Errorf("tool %s not found", name)
}

// CallToolWithArgs is a convenience method to call a tool with arguments
func (c *Client) CallToolWithArgs(ctx context.Context, toolName string, args any) (*tools.ToolCallResult, error) {
	argsBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	toolCall := tools.ToolCall{
		Type: "function",
		Function: tools.FunctionCall{
			Name:      toolName,
			Arguments: string(argsBytes),
		},
	}

	return c.CallTool(ctx, toolCall)
}

// is401Error checks if the error indicates a 401 Unauthorized response
func is401Error(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "401") ||
		strings.Contains(errMsg, "unauthorized") ||
		strings.Contains(errMsg, "authentication")
}

// fetchProtectedResourceMetadata attempts to fetch OAuth 2.0 Protected Resource Metadata
// from the server's well-known endpoint according to RFC 8705 and MCP 2025-DRAFT-v2
func (c *Client) fetchProtectedResourceMetadata(ctx context.Context) (*ProtectedResourceMetadata, error) {
	if c.serverURL == "" {
		return nil, fmt.Errorf("server URL not available for metadata fetching")
	}

	serverURL, err := url.Parse(c.serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	// Construct the well-known OAuth protected resource metadata endpoint
	metadataURL := &url.URL{
		Scheme: serverURL.Scheme,
		Host:   serverURL.Host,
		Path:   "/.well-known/oauth-authorization-server",
	}

	slog.Debug("Fetching OAuth Protected Resource Metadata", "url", metadataURL.String())

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata endpoint returned status %d", resp.StatusCode)
	}

	var metadata ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata response: %w", err)
	}

	return &metadata, nil
}

// createAuthorizationError processes 401 errors and returns an AuthorizationError with complete authorization URL
func (c *Client) createAuthorizationError(ctx context.Context, originalErr error) *AuthorizationError {
	if !is401Error(originalErr) {
		return nil
	}

	slog.Debug("Detected 401 authorization error, starting OAuth 2.0 authorization flow")

	// Step 1: Fetch OAuth Protected Resource Metadata
	metadata, err := c.fetchProtectedResourceMetadata(ctx)
	if err != nil {
		slog.Debug("Failed to fetch OAuth metadata", "error", err)
		return &AuthorizationError{
			Message:       "Authentication required but no authorization server information available",
			ServerURL:     c.serverURL,
			OriginalError: originalErr,
		}
	}

	if metadata.AuthorizationEndpoint == "" {
		slog.Debug("No authorization endpoint found in metadata")
		return &AuthorizationError{
			Message:             "Authentication required but no authorization endpoint available",
			AuthorizationServer: metadata.AuthorizationEndpoint,
			ServerURL:           c.serverURL,
			OriginalError:       originalErr,
		}
	}

	// Step 2: Perform Dynamic Client Registration (if endpoint available)
	var clientID, redirectURI string
	if metadata.RegistrationEndpoint != "" {
		slog.Debug("Performing dynamic client registration", "endpoint", metadata.RegistrationEndpoint)
		registrationResp, err := c.performClientRegistration(ctx, metadata.RegistrationEndpoint)
		if err != nil {
			slog.Debug("Dynamic client registration failed", "error", err)
			return &AuthorizationError{
				Message:             "Authentication required but client registration failed",
				AuthorizationServer: metadata.AuthorizationEndpoint,
				ServerURL:           c.serverURL,
				OriginalError:       originalErr,
			}
		}
		clientID = registrationResp.ClientID
		redirectURI = registrationResp.RedirectURIs[0] // Use the first registered redirect URI
	} else {
		slog.Debug("No registration endpoint available, using default values")
		// Fallback to default values when dynamic registration is not available
		clientID = "cagent-default-client"
		redirectURI = "urn:ietf:wg:oauth:2.0:oob"
	}

	// Step 3: Generate PKCE parameters
	pkceParams, err := generatePKCEParams()
	if err != nil {
		slog.Debug("Failed to generate PKCE parameters", "error", err)
		return &AuthorizationError{
			Message:             "Authentication required but failed to generate security parameters",
			AuthorizationServer: metadata.AuthorizationEndpoint,
			ServerURL:           c.serverURL,
			OriginalError:       originalErr,
		}
	}

	// Step 4: Generate state parameter
	state, err := generateState()
	if err != nil {
		slog.Debug("Failed to generate state parameter", "error", err)
		return &AuthorizationError{
			Message:             "Authentication required but failed to generate security parameters",
			AuthorizationServer: metadata.AuthorizationEndpoint,
			ServerURL:           c.serverURL,
			OriginalError:       originalErr,
		}
	}

	// Step 5: Build complete authorization URL
	authURLParams := &AuthorizationURLParams{
		AuthorizationEndpoint: metadata.AuthorizationEndpoint,
		ResponseType:          "code",
		ClientID:              clientID,
		RedirectURI:           redirectURI,
		CodeChallenge:         pkceParams.CodeChallenge,
		CodeChallengeMethod:   pkceParams.Method,
		State:                 state,
		Scope:                 "mcp",
	}

	authorizationURL, err := buildAuthorizationURL(authURLParams)
	if err != nil {
		slog.Debug("Failed to build authorization URL", "error", err)
		return &AuthorizationError{
			Message:             "Authentication required but failed to build authorization URL",
			AuthorizationServer: metadata.AuthorizationEndpoint,
			ServerURL:           c.serverURL,
			OriginalError:       originalErr,
		}
	}

	slog.Debug("Generated complete authorization URL", 
		"auth_url", authorizationURL, 
		"client_id", clientID,
		"redirect_uri", redirectURI,
		"state", state)

	return &AuthorizationError{
		Message:             "Authentication required",
		AuthorizationServer: metadata.AuthorizationEndpoint,
		ServerURL:           c.serverURL,
		AuthorizationURL:    authorizationURL,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeVerifier:        pkceParams.CodeVerifier,
		State:               state,
		OriginalError:       originalErr,
	}
}

// generatePKCEParams generates PKCE code verifier and challenge according to RFC 7636
func generatePKCEParams() (*PKCEParams, error) {
	// Generate code verifier (43-128 characters)
	verifierBytes := make([]byte, 32) // 32 bytes = 43 chars when base64url encoded
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}
	
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	
	// Generate code challenge using S256 method
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
	
	return &PKCEParams{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		Method:        "S256",
	}, nil
}

// generateState generates a random state parameter for OAuth 2.0
func generateState() (string, error) {
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(stateBytes), nil
}

// performClientRegistration performs OAuth 2.0 Dynamic Client Registration
func (c *Client) performClientRegistration(ctx context.Context, registrationEndpoint string) (*ClientRegistrationResponse, error) {
	if registrationEndpoint == "" {
		return nil, fmt.Errorf("registration endpoint not available")
	}

	// Create registration request
	request := ClientRegistrationRequest{
		RedirectURIs:            []string{"urn:ietf:wg:oauth:2.0:oob"}, // Out-of-band redirect for CLI clients
		TokenEndpointAuthMethod: "none", // Public client
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "cagent",
		ClientURI:               "https://github.com/docker/cagent",
		SoftwareID:              "cagent",
		SoftwareVersion:         "1.0.0",
		Scope:                   "mcp",
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal registration request: %w", err)
	}

	slog.Debug("Performing OAuth 2.0 Dynamic Client Registration", "endpoint", registrationEndpoint)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "POST", registrationEndpoint, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform registration request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("registration failed with status %d", resp.StatusCode)
	}

	var registrationResponse ClientRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&registrationResponse); err != nil {
		return nil, fmt.Errorf("failed to decode registration response: %w", err)
	}

	slog.Debug("Client registration successful", "client_id", registrationResponse.ClientID)
	return &registrationResponse, nil
}

// buildAuthorizationURL builds a complete OAuth 2.0 authorization URL with all required parameters
func buildAuthorizationURL(params *AuthorizationURLParams) (string, error) {
	if params.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("authorization endpoint is required")
	}
	if params.ClientID == "" {
		return "", fmt.Errorf("client_id is required")
	}
	if params.RedirectURI == "" {
		return "", fmt.Errorf("redirect_uri is required")
	}

	authURL, err := url.Parse(params.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization endpoint: %w", err)
	}

	query := authURL.Query()
	query.Set("response_type", params.ResponseType)
	query.Set("client_id", params.ClientID)
	query.Set("redirect_uri", params.RedirectURI)
	query.Set("code_challenge", params.CodeChallenge)
	query.Set("code_challenge_method", params.CodeChallengeMethod)
	query.Set("state", params.State)
	
	if params.Scope != "" {
		query.Set("scope", params.Scope)
	}
	
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}
