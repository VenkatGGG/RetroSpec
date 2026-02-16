package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var errInvalidArtifactToken = errors.New("invalid artifact token")

type artifactTokenClaims struct {
	ProjectID    string `json:"projectId"`
	SessionID    string `json:"sessionId"`
	ArtifactType string `json:"artifactType"`
	ExpiresAt    int64  `json:"exp"`
}

func (h *Handler) hasArtifactTokenSecret() bool {
	return strings.TrimSpace(h.artifactTokenSecret) != ""
}

func (h *Handler) signArtifactToken(
	projectID, sessionID, artifactType string,
	expiresAt time.Time,
) (string, error) {
	if !h.hasArtifactTokenSecret() {
		return "", errInvalidArtifactToken
	}

	claims := artifactTokenClaims{
		ProjectID:    strings.TrimSpace(projectID),
		SessionID:    strings.TrimSpace(sessionID),
		ArtifactType: strings.TrimSpace(artifactType),
		ExpiresAt:    expiresAt.UTC().Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := h.signArtifactPayload(encodedPayload)
	return encodedPayload + "." + signature, nil
}

func (h *Handler) verifyArtifactToken(rawToken string) (artifactTokenClaims, error) {
	if !h.hasArtifactTokenSecret() {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	parts := strings.Split(strings.TrimSpace(rawToken), ".")
	if len(parts) != 2 {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	encodedPayload := parts[0]
	signature := parts[1]
	expected := h.signArtifactPayload(encodedPayload)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	claims := artifactTokenClaims{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	if claims.ProjectID == "" || claims.SessionID == "" || claims.ArtifactType == "" {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}
	if claims.ExpiresAt < time.Now().UTC().Unix() {
		return artifactTokenClaims{}, errInvalidArtifactToken
	}

	return claims, nil
}

func (h *Handler) signArtifactPayload(encodedPayload string) string {
	mac := hmac.New(sha256.New, []byte(h.artifactTokenSecret))
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
