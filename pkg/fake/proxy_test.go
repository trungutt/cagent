package fake

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyHeaderUpdater(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		envKey         string
		envValue       string
		expectedHeader string
		expectedValue  string
	}{
		{
			name:           "OpenAI",
			host:           "https://api.openai.com/v1",
			envKey:         "OPENAI_API_KEY",
			envValue:       "test-openai-key",
			expectedHeader: "Authorization",
			expectedValue:  "Bearer test-openai-key",
		},
		{
			name:           "Anthropic",
			host:           "https://api.anthropic.com",
			envKey:         "ANTHROPIC_API_KEY",
			envValue:       "test-anthropic-key",
			expectedHeader: "X-Api-Key",
			expectedValue:  "test-anthropic-key",
		},
		{
			name:           "Google",
			host:           "https://generativelanguage.googleapis.com",
			envKey:         "GOOGLE_API_KEY",
			envValue:       "test-google-key",
			expectedHeader: "X-Goog-Api-Key",
			expectedValue:  "test-google-key",
		},
		{
			name:           "Mistral",
			host:           "https://api.mistral.ai/v1",
			envKey:         "MISTRAL_API_KEY",
			envValue:       "test-mistral-key",
			expectedHeader: "Authorization",
			expectedValue:  "Bearer test-mistral-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envValue)

			req, err := http.NewRequest(http.MethodPost, "https://example.com", http.NoBody)
			require.NoError(t, err)

			APIKeyHeaderUpdater(tt.host, req)

			assert.Equal(t, tt.expectedValue, req.Header.Get(tt.expectedHeader))
		})
	}
}

func TestAPIKeyHeaderUpdater_UnknownHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com", http.NoBody)
	require.NoError(t, err)

	APIKeyHeaderUpdater("https://unknown.host.com", req)

	assert.Empty(t, req.Header.Get("Authorization"))
	assert.Empty(t, req.Header.Get("X-Api-Key"))
}

func TestTargetURLForHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host     string
		expected bool
	}{
		{"https://api.openai.com/v1", true},
		{"https://api.anthropic.com", true},
		{"https://generativelanguage.googleapis.com", true},
		{"https://api.mistral.ai/v1", true},
		{"https://unknown.host.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()

			fn := TargetURLForHost(tt.host)
			if tt.expected {
				assert.NotNil(t, fn)
			} else {
				assert.Nil(t, fn)
			}
		})
	}
}

func TestExtractFirstUserMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "simple string content",
			body:     `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":"Hello world"}]}`,
			expected: "Hello world",
		},
		{
			name:     "content blocks with text",
			body:     `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"What is Docker?"}]}]}`,
			expected: "What is Docker?",
		},
		{
			name:     "content blocks with cache_control (should be ignored)",
			body:     `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"What is Docker?","cache_control":{"type":"ephemeral"}}]}]}`,
			expected: "What is Docker?",
		},
		{
			name:     "multiple user messages - returns first only",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":"First"},{"role":"assistant","content":"Response"},{"role":"user","content":"Second"}]}`,
			expected: "First",
		},
		{
			name:     "invalid JSON",
			body:     `not json`,
			expected: "",
		},
		{
			name:     "empty body",
			body:     `{}`,
			expected: "",
		},
		{
			name:     "ignores system and assistant messages",
			body:     `{"model":"claude-3","messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"Hi"},{"role":"assistant","content":"Hello"}]}`,
			expected: "Hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := extractFirstUserMessage(tt.body)
			assert.Equal(t, tt.expected, msg)
		})
	}
}

func TestExtractFirstUserMessage_MatchesAcrossVersions(t *testing.T) {
	t.Parallel()

	// Request without cache_control (old cagent version)
	oldBody := `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"What's a Dockerfile?"}]}],"system":[{"text":"You are helpful"}],"tools":[]}`

	// Request with cache_control (new cagent version)
	newBody := `{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"What's a Dockerfile?","cache_control":{"type":"ephemeral"}}]}],"system":[{"text":"You are helpful","cache_control":{"type":"ephemeral"}}],"tools":[],"stream":true}`

	oldMsg := extractFirstUserMessage(oldBody)
	newMsg := extractFirstUserMessage(newBody)

	assert.Equal(t, oldMsg, newMsg, "messages should match across versions")
	assert.Equal(t, "What's a Dockerfile?", oldMsg)
}
