package store

import "time"

type Session struct {
	ID              string             `json:"id"`
	ProjectID       string             `json:"projectId"`
	Site            string             `json:"site"`
	Route           string             `json:"route"`
	StartedAt       time.Time          `json:"startedAt"`
	DurationMs      int                `json:"durationMs"`
	EventsObjectKey string             `json:"eventsObjectKey"`
	CreatedAt       time.Time          `json:"createdAt"`
	Markers         []ErrorMark        `json:"markers"`
	Artifacts       []SessionArtifact  `json:"artifacts"`
	ReportCard      *SessionReportCard `json:"reportCard,omitempty"`
}

type ErrorMark struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"sessionId"`
	ClusterKey     string    `json:"clusterKey"`
	Label          string    `json:"label"`
	ReplayOffsetMs int       `json:"replayOffsetMs"`
	Kind           string    `json:"kind"`
	ObservedAt     time.Time `json:"observedAt"`
}

type IssueCluster struct {
	ProjectID               string    `json:"projectId"`
	Key                     string    `json:"key"`
	Symptom                 string    `json:"symptom"`
	SessionCount            int       `json:"sessionCount"`
	UserCount               int       `json:"userCount"`
	RepresentativeSessionID string    `json:"representativeSessionId"`
	Confidence              float64   `json:"confidence"`
	LastSeenAt              time.Time `json:"lastSeenAt"`
	CreatedAt               time.Time `json:"createdAt"`
}

type IssueKindStat struct {
	Kind         string    `json:"kind"`
	MarkerCount  int       `json:"markerCount"`
	SessionCount int       `json:"sessionCount"`
	ClusterCount int       `json:"clusterCount"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
}

type IngestPayload struct {
	Session SessionInput       `json:"session"`
	Markers []ErrorMarkerInput `json:"markers"`
}

type SessionInput struct {
	ID              string    `json:"id"`
	Site            string    `json:"site"`
	Route           string    `json:"route"`
	StartedAt       time.Time `json:"startedAt"`
	DurationMs      int       `json:"durationMs"`
	EventsObjectKey string    `json:"eventsObjectKey"`
}

type ErrorMarkerInput struct {
	ID             string `json:"id"`
	ClusterKey     string `json:"clusterKey"`
	Label          string `json:"label"`
	ReplayOffsetMs int    `json:"replayOffsetMs"`
	Kind           string `json:"kind"`
}

type PromoteResult struct {
	Promoted []IssueCluster `json:"promoted"`
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Site      string    `json:"site"`
	CreatedAt time.Time `json:"createdAt"`
}

type ProjectAPIKey struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"projectId"`
	Label      string     `json:"label"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type ArtifactWindow struct {
	StartMs int `json:"startMs"`
	EndMs   int `json:"endMs"`
}

type SessionArtifact struct {
	ID           string           `json:"id"`
	ProjectID    string           `json:"projectId"`
	SessionID    string           `json:"sessionId"`
	ArtifactType string           `json:"artifactType"`
	ArtifactKey  string           `json:"artifactKey"`
	TriggerKind  string           `json:"triggerKind"`
	Status       string           `json:"status"`
	GeneratedAt  time.Time        `json:"generatedAt"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	Windows      []ArtifactWindow `json:"windows"`
}

type SessionReportCard struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"projectId"`
	SessionID          string    `json:"sessionId"`
	Status             string    `json:"status"`
	Symptom            string    `json:"symptom"`
	TechnicalRootCause string    `json:"technicalRootCause"`
	SuggestedFix       string    `json:"suggestedFix"`
	TextSummary        string    `json:"textSummary"`
	VisualSummary      string    `json:"visualSummary"`
	Confidence         float64   `json:"confidence"`
	GeneratedAt        time.Time `json:"generatedAt"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type CleanupResult struct {
	DeletedSessions            int      `json:"deletedSessions"`
	DeletedIssueClusters       int      `json:"deletedIssueClusters"`
	DeletedEventObjects        int      `json:"deletedEventObjects"`
	DeletedArtifactObjects     int      `json:"deletedArtifactObjects"`
	DeletedEventObjectKeys     []string `json:"-"`
	DeletedArtifactObjectKeys  []string `json:"-"`
	FailedEventObjectDelete    int      `json:"failedEventObjectDelete"`
	FailedArtifactObjectDelete int      `json:"failedArtifactObjectDelete"`
	RetentionDays              int      `json:"retentionDays"`
}
