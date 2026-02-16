package api

import (
	"testing"
	"time"
)

func TestArtifactTokenRoundTrip(t *testing.T) {
	handler := &Handler{
		artifactTokenSecret: "test-secret",
	}

	expiresAt := time.Now().UTC().Add(2 * time.Minute)
	token, err := handler.signArtifactToken("proj_abc", "session_1", "replay_video", expiresAt)
	if err != nil {
		t.Fatalf("expected token to be generated: %v", err)
	}

	claims, err := handler.verifyArtifactToken(token)
	if err != nil {
		t.Fatalf("expected token to verify: %v", err)
	}

	if claims.ProjectID != "proj_abc" || claims.SessionID != "session_1" || claims.ArtifactType != "replay_video" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestArtifactTokenRejectsExpired(t *testing.T) {
	handler := &Handler{
		artifactTokenSecret: "test-secret",
	}

	token, err := handler.signArtifactToken(
		"proj_abc",
		"session_1",
		"replay_video",
		time.Now().UTC().Add(-1*time.Minute),
	)
	if err != nil {
		t.Fatalf("expected token to be generated: %v", err)
	}

	if _, err := handler.verifyArtifactToken(token); err == nil {
		t.Fatal("expected expired token to fail verification")
	}
}
