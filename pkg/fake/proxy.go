// Package fake provides a VCR-based proxy for replaying recorded AI API responses.
// This is useful for E2E testing without making real API calls.
package fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// StartProxy starts an internal HTTP proxy that replays cassette responses.
// It returns the proxy URL and a cleanup function that should be called when done.
func StartProxy(cassettePath string) (string, func() error, error) {
	return StartProxyWithOptions(cassettePath, recorder.ModeReplayOnly, nil, nil)
}

// StartRecordingProxy starts a proxy that records AI API interactions to a cassette file.
// It injects API keys from environment variables for the actual API calls.
// The recorded cassette can later be replayed using StartProxy.
func StartRecordingProxy(cassettePath string) (string, func() error, error) {
	return StartProxyWithOptions(cassettePath, recorder.ModeRecordOnce, nil, APIKeyHeaderUpdater)
}

// APIKeyHeaderUpdater injects API keys from environment variables into request headers.
// This is used when recording API interactions to ensure real API calls succeed.
func APIKeyHeaderUpdater(host string, req *http.Request) {
	switch host {
	case "https://api.openai.com/v1":
		req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	case "https://api.anthropic.com":
		req.Header.Del("Authorization")
		req.Header.Set("X-Api-Key", os.Getenv("ANTHROPIC_API_KEY"))
	case "https://generativelanguage.googleapis.com":
		req.Header.Del("Authorization")
		req.Header.Set("X-Goog-Api-Key", os.Getenv("GOOGLE_API_KEY"))
	case "https://api.mistral.ai/v1":
		req.Header.Set("Authorization", "Bearer "+os.Getenv("MISTRAL_API_KEY"))
	}
}

// StartProxyWithOptions starts an internal HTTP proxy with configurable options.
// - mode: recorder mode (ModeReplayOnly, ModeRecordOnce, etc.)
// - matcher: custom matcher function (nil uses default CustomMatcher)
// - headerUpdater: optional function to update request headers (for recording with real API keys)
func StartProxyWithOptions(
	cassettePath string,
	mode recorder.Mode,
	matcher recorder.MatcherFunc,
	headerUpdater func(host string, req *http.Request),
) (string, func() error, error) {
	if matcher == nil {
		matcher = CustomMatcher(nil)
	}

	transport, err := recorder.New(cassettePath,
		recorder.WithMode(mode),
		recorder.WithMatcher(matcher),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(RemoveHeadersHook, recorder.AfterCaptureHook),
		recorder.WithReplayableInteractions(false), // Each cassette entry consumed once
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create VCR recorder: %w", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Any("/*", Handle(transport, headerUpdater))

	httpServer := httptest.NewServer(e)

	cleanup := func() error {
		httpServer.Close()
		return transport.Stop()
	}

	return httpServer.URL, cleanup, nil
}

// RemoveHeadersHook strips headers from recorded interactions for security.
func RemoveHeadersHook(i *cassette.Interaction) error {
	i.Request.Headers = map[string][]string{}
	i.Response.Headers = map[string][]string{}
	return nil
}

// extractFirstUserMessage extracts the first user message text from a request body.
func extractFirstUserMessage(body string) string {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return ""
	}

	// Extract text from the FIRST user message only
	for _, msg := range req.Messages {
		if msg.Role != "user" {
			continue
		}

		switch content := msg.Content.(type) {
		case string:
			return content
		case []any:
			for _, block := range content {
				if blockMap, ok := block.(map[string]any); ok {
					if text, ok := blockMap["text"].(string); ok {
						return text
					}
				}
			}
		}
		break // Only first user message
	}

	return ""
}

// CustomMatcher creates a matcher that compares only the first user message.
// The onError callback is called if reading the request body fails (nil logs and returns false).
func CustomMatcher(onError func(err error)) recorder.MatcherFunc {
	return func(r *http.Request, i cassette.Request) bool {
		if r.Body == nil || r.Body == http.NoBody {
			return cassette.DefaultMatcher(r, i)
		}

		reqBody, err := io.ReadAll(r.Body)
		if err != nil {
			if onError != nil {
				onError(err)
			} else {
				slog.Error("Failed to read request body for matching", "error", err)
			}
			return false
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(reqBody))

		// Extract first user message from both
		reqMsg := extractFirstUserMessage(string(reqBody))
		cassetteMsg := extractFirstUserMessage(i.Body)

		// If both have user messages, compare them
		if reqMsg != "" && cassetteMsg != "" {
			return reqMsg == cassetteMsg
		}

		// Fallback to full body comparison for non-chat requests
		return string(reqBody) == i.Body
	}
}

// TargetURLForHost returns the target URL builder for a given forwarding host.
// Returns nil if the host is not recognized.
func TargetURLForHost(host string) func(req *http.Request) string {
	switch host {
	case "https://api.openai.com/v1":
		return func(req *http.Request) string {
			return "https://api.openai.com" + req.URL.Redacted()
		}
	case "https://api.anthropic.com":
		return func(req *http.Request) string {
			return "https://api.anthropic.com" + req.URL.Redacted()
		}
	case "https://generativelanguage.googleapis.com":
		return func(req *http.Request) string {
			return "https://generativelanguage.googleapis.com" + req.URL.Redacted()
		}
	case "https://api.mistral.ai/v1":
		return func(req *http.Request) string {
			return "https://api.mistral.ai" + req.URL.Redacted()
		}
	default:
		return nil
	}
}

// Handle creates an echo handler that proxies requests through the VCR transport.
// The headerUpdater is called with the host and request to update headers (e.g., for adding API keys).
func Handle(transport http.RoundTripper, headerUpdater func(host string, req *http.Request)) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		host := c.Request().Header.Get("X-Cagent-Forward")
		host = strings.TrimSuffix(host, "/")

		toTargetURL := TargetURLForHost(host)
		if toTargetURL == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "unknown service host "+host)
		}

		targetURL := toTargetURL(c.Request())

		req, err := http.NewRequestWithContext(ctx, c.Request().Method, targetURL, c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create new request")
		}

		maps.Copy(req.Header, c.Request().Header)

		if headerUpdater != nil {
			headerUpdater(host, req)
		}

		client := &http.Client{
			Timeout:   0, // no timeout, let ctx control it
			Transport: transport,
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("VCR proxy request failed", "url", targetURL, "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to run request: "+err.Error())
		}
		defer resp.Body.Close()

		maps.Copy(c.Response().Header(), resp.Header)

		c.Response().WriteHeader(resp.StatusCode)

		if IsStreamResponse(resp) {
			return StreamCopy(c, resp)
		}

		_, err = io.Copy(c.Response().Writer, resp.Body)
		return err
	}
}

// StreamCopy copies a streaming response to the client.
func StreamCopy(c echo.Context, resp *http.Response) error {
	ctx := c.Request().Context()

	writer := c.Response().Writer.(io.ReaderFrom)

	for {
		select {
		case <-ctx.Done():
			slog.WarnContext(ctx, "client disconnected, stop streaming")
			return nil
		default:
			n, err := writer.ReadFrom(io.LimitReader(resp.Body, 256))
			if n > 0 {
				c.Response().Flush() // keep flushing to client
			}
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return nil
				}
				slog.ErrorContext(ctx, "stream read error", "error", err)
				return err
			}
		}
	}
}

// IsStreamResponse checks if the response should be streamed.
func IsStreamResponse(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		return true
	}

	te := strings.ToLower(resp.Header.Get("Transfer-Encoding"))
	if strings.Contains(te, "chunked") && !strings.Contains(ct, "application/json") {
		return true
	}

	return strings.Contains(ct, "application/octet-stream") ||
		strings.Contains(ct, "application/x-ndjson") ||
		strings.Contains(ct, "application/stream+json")
}
