package api

import (
	"time"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/session"
)

type Message struct {
	Role         chat.MessageRole   `json:"role"`
	Content      string             `json:"content"`
	MultiContent []chat.MessagePart `json:"multi_content,omitempty"`
}

// Agent represents an agent in the API
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Multi       bool   `json:"multi"`
}

// CreateAgentRequest represents a request to create an agent
type CreateAgentRequest struct {
	Prompt string `json:"prompt"`
}

// CreateAgentResponse represents the response from creating an agent
type CreateAgentResponse struct {
	Path string `json:"path"`
	Out  string `json:"out"`
}

// CreateAgentConfigRequest represents a request to create an agent manually
type CreateAgentConfigRequest struct {
	Filename    string `json:"filename"`
	Model       string `json:"model"`
	Description string `json:"description"`
	Instruction string `json:"instruction"`
}

// CreateAgentConfigResponse represents the response from creating an agent config
type CreateAgentConfigResponse struct {
	Filepath string `json:"filepath"`
}

// EditAgentConfigRequest represents a request to edit an agent config
type EditAgentConfigRequest struct {
	AgentConfig latest.Config `json:"agent_config"`
	Filename    string        `json:"filename"`
}

// EditAgentConfigResponse represents the response from editing an agent config
type EditAgentConfigResponse struct {
	Message string `json:"message"`
	Path    string `json:"path"`
	Config  any    `json:"config"`
}

// ImportAgentRequest represents a request to import an agent
type ImportAgentRequest struct {
	FilePath string `json:"file_path"`
}

// ImportAgentResponse represents the response from importing an agent
type ImportAgentResponse struct {
	OriginalPath string `json:"originalPath"`
	TargetPath   string `json:"targetPath"`
	Description  string `json:"description"`
}

// ExportAgentsResponse represents the response from exporting agents
type ExportAgentsResponse struct {
	ZipPath      string `json:"zipPath"`
	ZipFile      string `json:"zipFile"`
	ZipDirectory string `json:"zipDirectory"`
	AgentsDir    string `json:"agentsDir"`
	CreatedAt    string `json:"createdAt"`
}

// PullAgentRequest represents a request to pull an agent
type PullAgentRequest struct {
	Name string `json:"name"`
}

// PullAgentResponse represents the response from pulling an agent
type PullAgentResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PushAgentRequest represents a request to push an agent
type PushAgentRequest struct {
	Filepath string `json:"filepath"`
	Tag      string `json:"tag"`
}

// PushAgentResponse represents the response from pushing an agent
type PushAgentResponse struct {
	Filepath string `json:"filepath"`
	Tag      string `json:"tag"`
	Digest   string `json:"digest"`
}

// DeleteAgentRequest represents a request to delete an agent
type DeleteAgentRequest struct {
	FilePath string `json:"file_path"`
}

// DeleteAgentResponse represents the response from deleting an agent
type DeleteAgentResponse struct {
	FilePath string `json:"filePath"`
}

// SessionsResponse represents a session in the sessions list
type SessionsResponse struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	CreatedAt    string `json:"created_at"`
	NumMessages  int    `json:"num_messages"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	WorkingDir   string `json:"working_dir,omitempty"`
}

// SessionResponse represents a detailed session
type SessionResponse struct {
	ID            string                     `json:"id"`
	Title         string                     `json:"title"`
	Messages      []session.Message          `json:"messages,omitempty"`
	CreatedAt     time.Time                  `json:"created_at"`
	ToolsApproved bool                       `json:"tools_approved"`
	InputTokens   int64                      `json:"input_tokens"`
	OutputTokens  int64                      `json:"output_tokens"`
	WorkingDir    string                     `json:"working_dir,omitempty"`
	Permissions   *session.PermissionsConfig `json:"permissions,omitempty"`
}

// UpdateSessionPermissionsRequest represents a request to update session permissions.
type UpdateSessionPermissionsRequest struct {
	Permissions *session.PermissionsConfig `json:"permissions"`
}

// ResumeSessionRequest represents a request to resume a session
type ResumeSessionRequest struct {
	Confirmation string `json:"confirmation"`
}

// DesktopTokenResponse represents the response from getting a desktop token
type DesktopTokenResponse struct {
	Token string `json:"token"`
}

// ResumeElicitationRequest represents a request to resume with an elicitation response
type ResumeElicitationRequest struct {
	Action  string         `json:"action"`  // "accept", "decline", or "cancel"
	Content map[string]any `json:"content"` // The submitted form data (only present when action is "accept")
}

// ExportedSession represents a shareable session format with metadata.
// This format is designed to be versioned and portable across different
// cagent instances and can be shared via Docker Hub or other OCI registries.
type ExportedSession struct {
	// Version is the export format version for compatibility checking
	Version string `json:"version"`
	// ExportedAt is the timestamp when the session was exported
	ExportedAt string `json:"exported_at"`
	// Session contains the full session data
	Session *session.Session `json:"session"`
}

// ExportSessionResponse represents the response from exporting a session
type ExportSessionResponse struct {
	// Data contains the exported session in shareable format
	Data ExportedSession `json:"data"`
}

// PushSessionRequest represents a request to push a session to an OCI registry
type PushSessionRequest struct {
	// Reference is the OCI registry reference (e.g., "docker.io/user/sessions:my-session")
	Reference string `json:"reference"`
}

// PushSessionResponse represents the response from pushing a session
type PushSessionResponse struct {
	// Reference is the full registry reference where the session was pushed
	Reference string `json:"reference"`
	// Digest is the content digest of the pushed artifact
	Digest string `json:"digest"`
}

// PullSessionRequest represents a request to pull a session from an OCI registry
type PullSessionRequest struct {
	// Reference is the OCI registry reference to pull from
	Reference string `json:"reference"`
}

// PullSessionResponse represents the response from pulling a session
type PullSessionResponse struct {
	// Reference is the registry reference that was pulled
	Reference string `json:"reference"`
	// Digest is the content digest of the pulled artifact
	Digest string `json:"digest"`
	// Message provides status information
	Message string `json:"message"`
}

// ImportSessionRequest represents a request to import a session from the content store
type ImportSessionRequest struct {
	// Reference is the content store reference to import from (set after pulling)
	Reference string `json:"reference"`
}

// ImportSessionResponse represents the response from importing a session
type ImportSessionResponse struct {
	// ID is the new session ID assigned to the imported session
	ID string `json:"id"`
	// Title is the title of the imported session
	Title string `json:"title"`
	// Message provides status information about the import
	Message string `json:"message"`
}
