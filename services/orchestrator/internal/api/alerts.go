package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"retrospec/services/orchestrator/internal/store"
)

type issueAlertNotifier struct {
	store         *store.Postgres
	webhookURL    string
	authHeader    string
	minConfidence float64
	cooldown      time.Duration
	client        *http.Client
}

func newIssueAlertNotifier(
	store *store.Postgres,
	webhookURL string,
	authHeader string,
	minConfidence float64,
	cooldownMinutes int,
) *issueAlertNotifier {
	if minConfidence < 0 {
		minConfidence = 0
	}
	if minConfidence > 1 {
		minConfidence = 1
	}
	if cooldownMinutes < 0 {
		cooldownMinutes = 0
	}

	return &issueAlertNotifier{
		store:         store,
		webhookURL:    strings.TrimSpace(webhookURL),
		authHeader:    strings.TrimSpace(authHeader),
		minConfidence: minConfidence,
		cooldown:      time.Duration(cooldownMinutes) * time.Minute,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (n *issueAlertNotifier) enabled() bool {
	return n != nil && strings.TrimSpace(n.webhookURL) != ""
}

func (n *issueAlertNotifier) notifyClusterPromoted(
	ctx context.Context,
	cluster store.IssueCluster,
	trigger string,
) (bool, error) {
	if !n.enabled() {
		return false, nil
	}
	if cluster.Confidence < n.minConfidence {
		return false, nil
	}
	state := strings.TrimSpace(cluster.State)
	if state == "resolved" || state == "muted" {
		return false, nil
	}

	alertType := "cluster_promoted"
	if n.cooldown > 0 {
		lastSentAt, err := n.store.LastIssueAlertAt(ctx, cluster.ProjectID, cluster.Key, alertType)
		if err != nil {
			return false, err
		}
		if lastSentAt != nil && time.Since(*lastSentAt) < n.cooldown {
			return false, nil
		}
	}

	payload := map[string]any{
		"event":     "issue_cluster_promoted",
		"trigger":   trigger,
		"sentAt":    time.Now().UTC().Format(time.RFC3339),
		"projectId": cluster.ProjectID,
		"cluster": map[string]any{
			"key":                     cluster.Key,
			"symptom":                 cluster.Symptom,
			"sessionCount":            cluster.SessionCount,
			"userCount":               cluster.UserCount,
			"confidence":              cluster.Confidence,
			"lastSeenAt":              cluster.LastSeenAt.Format(time.RFC3339),
			"representativeSessionId": cluster.RepresentativeSessionID,
			"state":                   cluster.State,
			"assignee":                cluster.Assignee,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	if n.authHeader != "" {
		request.Header.Set("Authorization", n.authHeader)
	}

	response, err := n.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return false, fmt.Errorf("webhook status=%d body=%s", response.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	if err := n.store.RecordIssueAlert(ctx, cluster.ProjectID, cluster.Key, alertType, payload, time.Now().UTC()); err != nil {
		return false, err
	}

	return true, nil
}
