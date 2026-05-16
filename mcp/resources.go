package mcp

import (
	"context"
	"fmt"
	"strings"
)

// ResourceDescriptor is what resources/list returns. Mirrors the MCP shape.
type ResourceDescriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// guideResources is the per-server context for the game://.../guide
// resource family.
type guideResources struct {
	reg *UserAwareRegistry
}

// WireGuideResources installs the game://owner/name/guide handlers on
// the server. Idempotent across server instances.
func (s *Server) WireGuideResources(reg *UserAwareRegistry) {
	s.mu.Lock()
	s.guideResources = &guideResources{reg: reg}
	s.mu.Unlock()
}

// listGuideResources returns ResourceDescriptors for every game the
// authenticated caller owns that has a non-empty LLMGuide. The URI
// scheme is game://owner/name/guide.
func (s *Server) listGuideResources(ctx context.Context) ([]ResourceDescriptor, error) {
	s.mu.RLock()
	gr := s.guideResources
	s.mu.RUnlock()
	if gr == nil {
		return nil, nil
	}
	userID := UserIDFromContext(ctx)
	store := gr.reg.Store()
	names, err := store.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceDescriptor, 0, len(names))
	for _, n := range names {
		ug, err := store.Get(ctx, userID, n)
		if err != nil || ug == nil || ug.LLMGuide == "" {
			continue
		}
		out = append(out, ResourceDescriptor{
			URI:         "game://" + userID + "/" + n + "/guide",
			Name:        n + " — guide",
			Description: "Designer's notes on rules and strategy for " + n,
			MimeType:    "text/markdown",
		})
	}
	return out, nil
}

// readGuideResource fetches the LLMGuide for the given URI. The
// authenticated caller must be the owner; any other user gets an error.
func (s *Server) readGuideResource(ctx context.Context, uri string) (string, error) {
	s.mu.RLock()
	gr := s.guideResources
	s.mu.RUnlock()
	if gr == nil {
		return "", fmt.Errorf("resources not wired")
	}
	if !strings.HasPrefix(uri, "game://") {
		return "", fmt.Errorf("bad uri: %s", uri)
	}
	tail := strings.TrimPrefix(uri, "game://")
	parts := strings.Split(tail, "/")
	if len(parts) != 3 || parts[2] != "guide" {
		return "", fmt.Errorf("bad uri shape: %s", uri)
	}
	owner, name := parts[0], parts[1]

	caller := UserIDFromContext(ctx)
	if caller != owner {
		return "", fmt.Errorf("forbidden: %s does not own %s/%s", caller, owner, name)
	}
	store := gr.reg.Store()
	ug, err := store.Get(ctx, owner, name)
	if err != nil {
		return "", err
	}
	if ug == nil {
		return "", fmt.Errorf("not found")
	}
	return ug.LLMGuide, nil
}
