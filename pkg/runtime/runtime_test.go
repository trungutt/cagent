package runtime

import (
	"errors"
	"testing"

	"github.com/docker/cagent/pkg/tools/mcp"
)

func TestExtractAuthorizationError(t *testing.T) {
	r := &Runtime{} // Empty runtime for testing
	
	tests := []struct {
		name     string
		err      error
		expected *mcp.AuthorizationError
	}{
		{
			name: "direct authorization error",
			err: &mcp.AuthorizationError{
				Message:             "Authentication required",
				AuthorizationServer: "https://auth.example.com",
				ServerURL:           "https://mcp.example.com",
				OriginalError:       errors.New("HTTP 401 Unauthorized"),
			},
			expected: &mcp.AuthorizationError{
				Message:             "Authentication required",
				AuthorizationServer: "https://auth.example.com",
				ServerURL:           "https://mcp.example.com",
				OriginalError:       errors.New("HTTP 401 Unauthorized"),
			},
		},
		{
			name: "wrapped authorization error",
			err: errors.Join(
				errors.New("failed to start toolset"),
				&mcp.AuthorizationError{
					Message:             "Authentication required",
					AuthorizationServer: "https://auth.example.com",
					ServerURL:           "https://mcp.example.com",
				},
			),
			expected: &mcp.AuthorizationError{
				Message:             "Authentication required",
				AuthorizationServer: "https://auth.example.com",
				ServerURL:           "https://mcp.example.com",
			},
		},
		{
			name:     "non-authorization error",
			err:      errors.New("some other error"),
			expected: nil,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.extractAuthorizationError(tt.err)
			
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil but got %v", result)
				}
				return
			}
			
			if result == nil {
				t.Errorf("expected AuthorizationError but got nil")
				return
			}
			
			if result.Message != tt.expected.Message {
				t.Errorf("expected Message %q, got %q", tt.expected.Message, result.Message)
			}
			
			if result.AuthorizationServer != tt.expected.AuthorizationServer {
				t.Errorf("expected AuthorizationServer %q, got %q", tt.expected.AuthorizationServer, result.AuthorizationServer)
			}
			
			if result.ServerURL != tt.expected.ServerURL {
				t.Errorf("expected ServerURL %q, got %q", tt.expected.ServerURL, result.ServerURL)
			}
		})
	}
}