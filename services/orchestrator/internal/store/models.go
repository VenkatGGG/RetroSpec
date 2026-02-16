package store

import "time"

type Session struct {
	ID              string      `json:"id"`
	Site            string      `json:"site"`
	Route           string      `json:"route"`
	StartedAt       time.Time   `json:"startedAt"`
	DurationMs      int         `json:"durationMs"`
	EventsObjectKey string      `json:"eventsObjectKey"`
	CreatedAt       time.Time   `json:"createdAt"`
	Markers         []ErrorMark `json:"markers"`
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
	Key                     string    `json:"key"`
	Symptom                 string    `json:"symptom"`
	SessionCount            int       `json:"sessionCount"`
	UserCount               int       `json:"userCount"`
	RepresentativeSessionID string    `json:"representativeSessionId"`
	Confidence              float64   `json:"confidence"`
	LastSeenAt              time.Time `json:"lastSeenAt"`
	CreatedAt               time.Time `json:"createdAt"`
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

type CleanupResult struct {
	DeletedSessions         int      `json:"deletedSessions"`
	DeletedIssueClusters    int      `json:"deletedIssueClusters"`
	DeletedEventObjects     int      `json:"deletedEventObjects"`
	DeletedEventObjectKeys  []string `json:"-"`
	FailedEventObjectDelete int      `json:"failedEventObjectDelete"`
	RetentionDays           int      `json:"retentionDays"`
}
