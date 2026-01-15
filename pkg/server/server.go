package server

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/session"
)

type Server struct {
	e  *echo.Echo
	sm *SessionManager
}

func New(ctx context.Context, sessionStore session.Store, runConfig *config.RuntimeConfig, refreshInterval time.Duration, agentSources config.Sources) (*Server, error) {
	e := echo.New()
	e.Use(middleware.CORS())
	e.Use(middleware.RequestLogger())

	s := &Server{
		e:  e,
		sm: NewSessionManager(ctx, agentSources, sessionStore, refreshInterval, runConfig),
	}

	group := e.Group("/api")

	// List all available agents
	group.GET("/agents", s.getAgents)
	// Get an agent by id
	group.GET("/agents/:id", s.getAgentConfig)

	// List all sessions
	group.GET("/sessions", s.getSessions)
	// Create a new session
	group.POST("/sessions", s.createSession)
	// Get a session by id
	group.GET("/sessions/:id", s.getSession)
	// Resume a session by id
	group.POST("/sessions/:id/resume", s.resumeSession)
	// Toggle YOLO mode for a session
	group.POST("/sessions/:id/tools/toggle", s.toggleSessionYolo)
	// Update session permissions
	group.PATCH("/sessions/:id/permissions", s.updateSessionPermissions)
	// Delete a session
	group.DELETE("/sessions/:id", s.deleteSession)
	// Export a session as JSON
	group.GET("/sessions/:id/export", s.exportSession)
	// Push a session to an OCI registry
	group.POST("/sessions/:id/push", s.pushSession)
	// Run an agent loop
	group.POST("/sessions/:id/agent/:agent", s.runAgent)
	group.POST("/sessions/:id/agent/:agent/:agent_name", s.runAgent)
	group.POST("/sessions/:id/elicitation", s.elicitation)

	// Session sharing (using distinct paths to avoid route conflicts with /sessions/:id)
	group.POST("/session-actions/pull", s.pullSession)
	group.POST("/session-actions/import", s.importSession)

	// Health check endpoint
	group.GET("/ping", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	return s, nil
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := http.Server{
		Handler: s.e,
	}

	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		slog.Error("Failed to start server", "error", err)
		return err
	}

	return nil
}

func (s *Server) getAgents(c echo.Context) error {
	agents := []api.Agent{}
	for k, agentSource := range s.sm.Sources {
		slog.Debug("API source", "source", agentSource.Name())

		c, err := config.Load(c.Request().Context(), agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		desc := c.Agents.First().Description

		switch {
		case len(c.Agents) > 1:
			agents = append(agents, api.Agent{
				Name:        k,
				Multi:       true,
				Description: desc,
			})
		case len(c.Agents) == 1:
			agents = append(agents, api.Agent{
				Name:        k,
				Multi:       false,
				Description: desc,
			})
		default:
			slog.Warn("No agents found in config from API source", "key", k)
			continue
		}
	}

	slices.SortFunc(agents, func(a, b api.Agent) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return c.JSON(http.StatusOK, agents)
}

func (s *Server) getAgentConfig(c echo.Context) error {
	agentID := c.Param("id")

	for k, agentSource := range s.sm.Sources {
		if k != agentID {
			continue
		}

		slog.Debug("API source", "source", agentSource.Name())
		cfg, err := config.Load(c.Request().Context(), agentSource)
		if err != nil {
			slog.Error("Failed to load config from API source", "key", k, "error", err)
			continue
		}

		return c.JSON(http.StatusOK, cfg)
	}

	return echo.NewHTTPError(http.StatusNotFound)
}

func (s *Server) getSessions(c echo.Context) error {
	sessions, err := s.sm.GetSessions(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get sessions: %v", err))
	}

	responses := make([]api.SessionsResponse, len(sessions))
	for i, sess := range sessions {
		responses[i] = api.SessionsResponse{
			ID:           sess.ID,
			Title:        sess.Title,
			CreatedAt:    sess.CreatedAt.Format(time.RFC3339),
			NumMessages:  len(sess.GetAllMessages()),
			InputTokens:  sess.InputTokens,
			OutputTokens: sess.OutputTokens,
			WorkingDir:   sess.WorkingDir,
		}
	}
	return c.JSON(http.StatusOK, responses)
}

func (s *Server) createSession(c echo.Context) error {
	var sessionTemplate session.Session
	if err := c.Bind(&sessionTemplate); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	sess, err := s.sm.CreateSession(c.Request().Context(), &sessionTemplate)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create session: %v", err))
	}

	return c.JSON(http.StatusOK, sess)
}

func (s *Server) getSession(c echo.Context) error {
	sess, err := s.sm.GetSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	return c.JSON(http.StatusOK, api.SessionResponse{
		ID:            sess.ID,
		Title:         sess.Title,
		CreatedAt:     sess.CreatedAt,
		Messages:      sess.GetAllMessages(),
		ToolsApproved: sess.ToolsApproved,
		InputTokens:   sess.InputTokens,
		OutputTokens:  sess.OutputTokens,
		WorkingDir:    sess.WorkingDir,
		Permissions:   sess.Permissions,
	})
}

func (s *Server) resumeSession(c echo.Context) error {
	var req api.ResumeSessionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.ResumeSession(c.Request().Context(), c.Param("id"), req.Confirmation); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to resume session: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session resumed"})
}

func (s *Server) toggleSessionYolo(c echo.Context) error {
	if err := s.sm.ToggleToolApproval(c.Request().Context(), c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to toggle session tool approval mode: %v", err))
	}
	return c.JSON(http.StatusOK, nil)
}

func (s *Server) updateSessionPermissions(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.UpdateSessionPermissionsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.UpdateSessionPermissions(c.Request().Context(), sessionID, req.Permissions); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to update session permissions: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session permissions updated"})
}

func (s *Server) deleteSession(c echo.Context) error {
	sessionID := c.Param("id")

	if err := s.sm.DeleteSession(c.Request().Context(), sessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to delete session: %v", err))
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "session deleted"})
}

func (s *Server) exportSession(c echo.Context) error {
	sess, err := s.sm.GetSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	exported := api.ExportedSession{
		Version:    oci.SessionExportVersion,
		ExportedAt: time.Now().Format(time.RFC3339),
		Session:    sess,
	}

	return c.JSON(http.StatusOK, api.ExportSessionResponse{Data: exported})
}

func (s *Server) pushSession(c echo.Context) error {
	sessionID := c.Param("id")

	var req api.PushSessionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if req.Reference == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "reference is required")
	}

	sess, err := s.sm.GetSession(c.Request().Context(), sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
	}

	store, err := content.NewStore()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create content store: %v", err))
	}

	// Package session as OCI artifact and store locally
	digest, err := oci.PackageSessionAsOCI(sess, req.Reference, store)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to package session: %v", err))
	}

	// Push to remote registry
	if err := remote.Push(req.Reference); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to push session to registry: %v", err))
	}

	reference := req.Reference
	if !strings.Contains(reference, ":") {
		reference += ":latest"
	}

	return c.JSON(http.StatusOK, api.PushSessionResponse{
		Reference: reference,
		Digest:    digest,
	})
}

func (s *Server) pullSession(c echo.Context) error {
	slog.Info("pullSession: handler called", "path", c.Request().URL.Path, "method", c.Request().Method)

	var req api.PullSessionRequest
	if err := c.Bind(&req); err != nil {
		slog.Error("pullSession: failed to bind request", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	slog.Info("pullSession: request parsed", "reference", req.Reference)

	if req.Reference == "" {
		slog.Error("pullSession: reference is empty")
		return echo.NewHTTPError(http.StatusBadRequest, "reference is required")
	}

	slog.Info("pullSession: pulling from registry", "reference", req.Reference)
	digest, err := remote.Pull(c.Request().Context(), req.Reference, false)
	if err != nil {
		slog.Error("pullSession: failed to pull from registry", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to pull session from registry: %v", err))
	}

	slog.Info("pullSession: success", "reference", req.Reference, "digest", digest)
	return c.JSON(http.StatusOK, api.PullSessionResponse{
		Reference: req.Reference,
		Digest:    digest,
		Message:   "session pulled successfully, use /api/session-actions/import to create it",
	})
}

func (s *Server) importSession(c echo.Context) error {
	slog.Info("importSession: handler called", "path", c.Request().URL.Path, "method", c.Request().Method)

	var req api.ImportSessionRequest
	if err := c.Bind(&req); err != nil {
		slog.Error("importSession: failed to bind request", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	slog.Info("importSession: request parsed", "reference", req.Reference)

	if req.Reference == "" {
		slog.Error("importSession: reference is empty")
		return echo.NewHTTPError(http.StatusBadRequest, "reference is required")
	}

	store, err := content.NewStore()
	if err != nil {
		slog.Error("importSession: failed to create content store", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create content store: %v", err))
	}

	slog.Info("importSession: extracting session from store", "reference", req.Reference)
	// Extract session from the stored artifact
	exported, err := oci.ExtractSessionFromStore(req.Reference, store)
	if err != nil {
		slog.Error("importSession: failed to extract session from store", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to extract session from store: %v", err))
	}

	if exported.Session == nil {
		slog.Error("importSession: session data is nil")
		return echo.NewHTTPError(http.StatusBadRequest, "invalid session data in artifact")
	}

	// Create a new session with a new ID (avoid conflicts with existing sessions)
	newSession := exported.Session
	newSession.ID = uuid.New().String()
	newSession.CreatedAt = time.Now()

	// Add the session to the store
	if err := s.sm.sessionStore.AddSession(c.Request().Context(), newSession); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create imported session: %v", err))
	}

	return c.JSON(http.StatusOK, api.ImportSessionResponse{
		ID:      newSession.ID,
		Title:   newSession.Title,
		Message: "session imported successfully",
	})
}

func (s *Server) runAgent(c echo.Context) error {
	sessionID := c.Param("id")
	agentFilename := c.Param("agent")
	currentAgent := cmp.Or(c.Param("agent_name"), "root")

	slog.Debug("Running agent", "agent_filename", agentFilename, "session_id", sessionID, "current_agent", currentAgent)

	var messages []api.Message
	if err := json.NewDecoder(c.Request().Body).Decode(&messages); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	streamChan, err := s.sm.RunSession(c.Request().Context(), sessionID, agentFilename, currentAgent, messages)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to run session: %v", err))
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)
	for event := range streamChan {
		data, err := json.Marshal(event)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to marshal event: %v", err))
		}
		fmt.Fprintf(c.Response(), "data: %s\n\n", string(data))
		c.Response().Flush()
	}

	return nil
}

func (s *Server) elicitation(c echo.Context) error {
	sessionID := c.Param("id")
	var req api.ResumeElicitationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := s.sm.ResumeElicitation(c.Request().Context(), sessionID, req.Action, req.Content); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to resume elicitation: %v", err))
	}

	return c.JSON(http.StatusOK, nil)
}
