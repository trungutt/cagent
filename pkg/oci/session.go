package oci

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/version"
)

const (
	// SessionExportVersion is the current version of the session export format
	SessionExportVersion = "1"
	// SessionMediaType is the media type for session artifacts
	SessionMediaType = "application/vnd.docker.cagent.session.v1+json"
)

// PackageSessionAsOCI creates an OCI artifact from a session and stores it in the content store
func PackageSessionAsOCI(sess *session.Session, artifactRef string, store *content.Store) (string, error) {
	if !strings.Contains(artifactRef, ":") {
		artifactRef += ":latest"
	}

	// Create the exported session structure
	exported := api.ExportedSession{
		Version:    SessionExportVersion,
		ExportedAt: time.Now().Format(time.RFC3339),
		Session:    sess,
	}

	// Serialize to JSON
	data, err := json.Marshal(exported)
	if err != nil {
		return "", fmt.Errorf("marshaling session: %w", err)
	}

	// Prepare OCI annotations
	annotations := map[string]string{
		"io.docker.cagent.version":             version.Version,
		"io.docker.cagent.artifact.type":       "session",
		"io.docker.cagent.session.id":          sess.ID,
		"io.docker.cagent.session.title":       sess.Title,
		"org.opencontainers.image.created":     time.Now().Format(time.RFC3339),
		"org.opencontainers.image.description": fmt.Sprintf("Cagent session: %s", sess.Title),
	}

	// Create OCI layer with session data
	layer := static.NewLayer(data, types.OCIUncompressedLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return "", fmt.Errorf("appending layer: %w", err)
	}

	// Convert to OCI manifest format to support annotations
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.Annotations(img, annotations).(v1.Image)

	// Store in content store
	digest, err := store.StoreArtifact(img, artifactRef)
	if err != nil {
		return "", fmt.Errorf("storing session artifact: %w", err)
	}

	return digest, nil
}

// ExtractSessionFromStore extracts a session from a stored OCI artifact
func ExtractSessionFromStore(identifier string, store *content.Store) (*api.ExportedSession, error) {
	// Get the artifact content
	data, err := store.GetArtifact(identifier)
	if err != nil {
		return nil, fmt.Errorf("getting artifact from store: %w", err)
	}

	// Parse the exported session
	var exported api.ExportedSession
	if err := json.Unmarshal([]byte(data), &exported); err != nil {
		return nil, fmt.Errorf("unmarshaling session: %w", err)
	}

	return &exported, nil
}
