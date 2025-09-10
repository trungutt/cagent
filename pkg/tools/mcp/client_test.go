package mcp

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

func TestIs401Error(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "401 status code",
			err:      errors.New("HTTP 401 Unauthorized"),
			expected: true,
		},
		{
			name:     "unauthorized message",
			err:      errors.New("request failed: unauthorized"),
			expected: true,
		},
		{
			name:     "authentication error",
			err:      errors.New("authentication required"),
			expected: true,
		},
		{
			name:     "400 status code",
			err:      errors.New("HTTP 400 Bad Request"),
			expected: false,
		},
		{
			name:     "500 status code",
			err:      errors.New("HTTP 500 Internal Server Error"),
			expected: false,
		},
		{
			name:     "connection error",
			err:      errors.New("connection refused"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := is401Error(tt.err)
			if result != tt.expected {
				t.Errorf("is401Error(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestCreateAuthorizationError(t *testing.T) {
	// Create a test client with a server URL
	client := &Client{
		serverURL: "https://example.com/mcp",
	}
	
	ctx := context.Background()
	
	tests := []struct {
		name          string
		originalError error
		expectAuthErr bool
	}{
		{
			name:          "401 error should create authorization error",
			originalError: errors.New("HTTP 401 Unauthorized"),
			expectAuthErr: true,
		},
		{
			name:          "unauthorized error should create authorization error",
			originalError: errors.New("request failed: unauthorized"),
			expectAuthErr: true,
		},
		{
			name:          "authentication error should create authorization error",
			originalError: errors.New("authentication required"),
			expectAuthErr: true,
		},
		{
			name:          "non-auth error should not create authorization error",
			originalError: errors.New("connection refused"),
			expectAuthErr: false,
		},
		{
			name:          "nil error should not create authorization error",
			originalError: nil,
			expectAuthErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authErr := client.createAuthorizationError(ctx, tt.originalError)
			
			if tt.expectAuthErr {
				if authErr == nil {
					t.Errorf("expected AuthorizationError but got nil")
					return
				}
				
				// Verify the error contains expected fields
				if authErr.ServerURL != client.serverURL {
					t.Errorf("expected ServerURL %s, got %s", client.serverURL, authErr.ServerURL)
				}
				
				if authErr.Message == "" {
					t.Errorf("expected non-empty Message")
				}
				
				if authErr.OriginalError != tt.originalError {
					t.Errorf("expected OriginalError to be preserved")
				}
			} else {
				if authErr != nil {
					t.Errorf("expected nil but got AuthorizationError: %v", authErr)
				}
			}
		})
	}
}

func TestGeneratePKCEParams(t *testing.T) {
	pkce, err := generatePKCEParams()
	if err != nil {
		t.Errorf("generatePKCEParams() failed: %v", err)
		return
	}

	// Verify code verifier is properly generated
	if len(pkce.CodeVerifier) == 0 {
		t.Errorf("code verifier should not be empty")
	}
	
	// Verify code challenge is properly generated
	if len(pkce.CodeChallenge) == 0 {
		t.Errorf("code challenge should not be empty")
	}
	
	// Verify method is S256
	if pkce.Method != "S256" {
		t.Errorf("expected method S256, got %s", pkce.Method)
	}
	
	// Verify code verifier length (should be 43 characters for 32 random bytes)
	if len(pkce.CodeVerifier) != 43 {
		t.Errorf("expected code verifier length 43, got %d", len(pkce.CodeVerifier))
	}
	
	// Verify code challenge length (should be 43 characters for SHA256)
	if len(pkce.CodeChallenge) != 43 {
		t.Errorf("expected code challenge length 43, got %d", len(pkce.CodeChallenge))
	}
}

func TestGenerateState(t *testing.T) {
	state1, err := generateState()
	if err != nil {
		t.Errorf("generateState() failed: %v", err)
		return
	}
	
	state2, err := generateState()
	if err != nil {
		t.Errorf("generateState() failed: %v", err)
		return
	}
	
	// Verify state is not empty
	if len(state1) == 0 {
		t.Errorf("state should not be empty")
	}
	
	// Verify states are different (randomness check)
	if state1 == state2 {
		t.Errorf("consecutive state generations should produce different values")
	}
	
	// Verify state length (16 bytes base64url encoded = 22 characters)
	if len(state1) != 22 {
		t.Errorf("expected state length 22, got %d", len(state1))
	}
}

func TestBuildAuthorizationURL(t *testing.T) {
	tests := []struct {
		name        string
		params      *AuthorizationURLParams
		expectError bool
		expectQuery map[string]string
	}{
		{
			name: "complete valid parameters",
			params: &AuthorizationURLParams{
				AuthorizationEndpoint: "https://auth.example.com/authorize",
				ResponseType:          "code",
				ClientID:              "test-client-123",
				RedirectURI:           "urn:ietf:wg:oauth:2.0:oob",
				CodeChallenge:         "test-challenge",
				CodeChallengeMethod:   "S256",
				State:                 "test-state",
				Scope:                 "mcp",
			},
			expectError: false,
			expectQuery: map[string]string{
				"response_type":          "code",
				"client_id":              "test-client-123",
				"redirect_uri":           "urn:ietf:wg:oauth:2.0:oob",
				"code_challenge":         "test-challenge",
				"code_challenge_method":  "S256",
				"state":                  "test-state",
				"scope":                  "mcp",
			},
		},
		{
			name: "missing authorization endpoint",
			params: &AuthorizationURLParams{
				ResponseType: "code",
				ClientID:     "test-client",
				RedirectURI:  "urn:ietf:wg:oauth:2.0:oob",
			},
			expectError: true,
		},
		{
			name: "missing client_id",
			params: &AuthorizationURLParams{
				AuthorizationEndpoint: "https://auth.example.com/authorize",
				ResponseType:          "code",
				RedirectURI:           "urn:ietf:wg:oauth:2.0:oob",
			},
			expectError: true,
		},
		{
			name: "missing redirect_uri",
			params: &AuthorizationURLParams{
				AuthorizationEndpoint: "https://auth.example.com/authorize",
				ResponseType:          "code",
				ClientID:              "test-client",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authURL, err := buildAuthorizationURL(tt.params)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			// Parse the URL to verify query parameters
			parsedURL, err := url.Parse(authURL)
			if err != nil {
				t.Errorf("failed to parse generated URL: %v", err)
				return
			}
			
			query := parsedURL.Query()
			for key, expectedValue := range tt.expectQuery {
				actualValue := query.Get(key)
				if actualValue != expectedValue {
					t.Errorf("expected query param %s=%s, got %s", key, expectedValue, actualValue)
				}
			}
		})
	}
}