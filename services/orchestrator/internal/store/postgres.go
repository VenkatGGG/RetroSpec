package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

var (
	clusterEmailRegex      = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}`)
	clusterUUIDRegex       = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
	clusterURLRegex        = regexp.MustCompile(`https?://[^\s]+`)
	clusterStatusCodeRegex = regexp.MustCompile(`\b([1-5])[0-9]{2}\b`)
	clusterNumberRegex     = regexp.MustCompile(`\b\d{2,}\b`)
	clusterHexRegex        = regexp.MustCompile(`\b[0-9a-f]{10,}\b`)
	clusterSpaceRegex      = regexp.MustCompile(`\s+`)
	clusterTokenRegex      = regexp.MustCompile(`[^a-z0-9._:-]+`)
)

func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctxPing); err != nil {
		pool.Close()
		return nil, err
	}

	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	p.pool.Close()
}

func (p *Postgres) Health(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *Postgres) Ingest(ctx context.Context, projectID string, payload IngestPayload) (Session, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)

	projectID = normalizeProjectID(projectID)

	sessionID := payload.Session.ID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	storedSession := Session{}
	err = tx.QueryRow(
		ctx,
		`INSERT INTO sessions (id, project_id, site, route, started_at, duration_ms, events_object_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (id) DO UPDATE
		 SET project_id = EXCLUDED.project_id,
		     site = EXCLUDED.site,
		     route = EXCLUDED.route,
		     started_at = EXCLUDED.started_at,
		     duration_ms = EXCLUDED.duration_ms,
		     events_object_key = EXCLUDED.events_object_key
		 RETURNING id, project_id, site, route, started_at, duration_ms, events_object_key, created_at`,
		sessionID,
		projectID,
		payload.Session.Site,
		payload.Session.Route,
		payload.Session.StartedAt,
		payload.Session.DurationMs,
		payload.Session.EventsObjectKey,
	).Scan(
		&storedSession.ID,
		&storedSession.ProjectID,
		&storedSession.Site,
		&storedSession.Route,
		&storedSession.StartedAt,
		&storedSession.DurationMs,
		&storedSession.EventsObjectKey,
		&storedSession.CreatedAt,
	)
	if err != nil {
		return Session{}, err
	}

	markers := make([]ErrorMark, 0, len(payload.Markers))
	for _, marker := range payload.Markers {
		markerID := marker.ID
		if markerID == "" {
			markerID = uuid.NewString()
		}
		kind := normalizeMarkerKind(marker.Kind)
		label := strings.TrimSpace(marker.Label)
		if label == "" {
			label = kind
		}
		replayOffsetMs := marker.ReplayOffsetMs
		if replayOffsetMs < 0 {
			replayOffsetMs = 0
		}
		clusterKey := deriveClusterKey(payload.Session.Route, kind, marker.ClusterKey, label)

		stored := ErrorMark{}
		err := tx.QueryRow(
			ctx,
			`INSERT INTO error_markers (id, session_id, cluster_key, label, replay_offset_ms, kind)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (id) DO UPDATE
			 SET cluster_key = EXCLUDED.cluster_key,
			     label = EXCLUDED.label,
			     replay_offset_ms = EXCLUDED.replay_offset_ms,
			     kind = EXCLUDED.kind
			 RETURNING id, session_id, cluster_key, label, replay_offset_ms, kind, observed_at`,
			markerID,
			sessionID,
			clusterKey,
			label,
			replayOffsetMs,
			kind,
		).Scan(
			&stored.ID,
			&stored.SessionID,
			&stored.ClusterKey,
			&stored.Label,
			&stored.ReplayOffsetMs,
			&stored.Kind,
			&stored.ObservedAt,
		)
		if err != nil {
			return Session{}, err
		}

		markers = append(markers, stored)
	}

	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}

	storedSession.Markers = markers
	return storedSession, nil
}

func (p *Postgres) PromoteClusters(ctx context.Context, projectID string, minSessions int) (PromoteResult, error) {
	if minSessions < 1 {
		return PromoteResult{}, fmt.Errorf("minSessions must be >= 1")
	}
	projectID = normalizeProjectID(projectID)

	rows, err := p.pool.Query(
		ctx,
		`WITH ranked AS (
		    SELECT
		      s.project_id,
		      em.cluster_key,
		      em.session_id,
		      em.label,
		      em.observed_at,
		      ROW_NUMBER() OVER (
		        PARTITION BY s.project_id, em.cluster_key
		        ORDER BY em.observed_at DESC, em.session_id
		      ) AS marker_rank
		    FROM error_markers em
		    JOIN sessions s ON s.id = em.session_id
		    WHERE em.cluster_key <> ''
		      AND s.project_id = $1
		),
		grouped AS (
		    SELECT
		      project_id,
		      cluster_key,
		      MAX(label) AS symptom,
		      COUNT(DISTINCT session_id) AS session_count,
		      COUNT(DISTINCT session_id) AS user_count,
		      MAX(observed_at) AS last_seen_at,
		      MAX(CASE WHEN marker_rank = 1 THEN session_id END) AS representative_session_id
		    FROM ranked
		    GROUP BY cluster_key
		)
		INSERT INTO issue_clusters (
		  project_id,
		  key,
		  symptom,
		  session_count,
		  user_count,
		  representative_session_id,
		  confidence,
		  last_seen_at
		)
		SELECT
		  g.project_id,
		  g.cluster_key,
		  g.symptom,
		  g.session_count,
		  g.user_count,
		  COALESCE(g.representative_session_id, ''),
		  LEAST(1.0, g.session_count::float / ($2::float + 1.0)) AS confidence,
		  g.last_seen_at
		FROM grouped g
		WHERE g.session_count >= $2
		ON CONFLICT (project_id, key) DO UPDATE
		SET symptom = EXCLUDED.symptom,
		    session_count = EXCLUDED.session_count,
		    user_count = EXCLUDED.user_count,
		    representative_session_id = EXCLUDED.representative_session_id,
		    confidence = EXCLUDED.confidence,
		    last_seen_at = EXCLUDED.last_seen_at
		RETURNING project_id, key, symptom, session_count, user_count, representative_session_id, confidence, last_seen_at, created_at`,
		projectID,
		minSessions,
	)
	if err != nil {
		return PromoteResult{}, err
	}
	defer rows.Close()

	result := PromoteResult{Promoted: []IssueCluster{}}
	for rows.Next() {
		var cluster IssueCluster
		if err := rows.Scan(
			&cluster.ProjectID,
			&cluster.Key,
			&cluster.Symptom,
			&cluster.SessionCount,
			&cluster.UserCount,
			&cluster.RepresentativeSessionID,
			&cluster.Confidence,
			&cluster.LastSeenAt,
			&cluster.CreatedAt,
		); err != nil {
			return PromoteResult{}, err
		}
		result.Promoted = append(result.Promoted, cluster)
	}

	if rows.Err() != nil {
		return PromoteResult{}, rows.Err()
	}

	return result, nil
}

func (p *Postgres) ListIssueClusters(ctx context.Context, projectID string) ([]IssueCluster, error) {
	projectID = normalizeProjectID(projectID)

	rows, err := p.pool.Query(
		ctx,
		`SELECT
		   ic.project_id,
		   ic.key,
		   ic.symptom,
		   ic.session_count,
		   ic.user_count,
		   ic.representative_session_id,
		   ic.confidence,
		   ic.last_seen_at,
		   ic.created_at,
		   CASE
		     WHEN ics.state = 'muted'
		      AND ics.muted_until IS NOT NULL
		      AND ics.muted_until <= NOW()
		       THEN 'open'
		     WHEN ics.state = 'resolved'
		      AND ics.updated_at IS NOT NULL
		      AND ic.last_seen_at > ics.updated_at
		       THEN 'open'
		     ELSE COALESCE(ics.state, 'open')
		   END AS state,
		   COALESCE(ics.assignee, '') AS assignee,
		   CASE
		     WHEN ics.state = 'muted'
		      AND ics.muted_until IS NOT NULL
		      AND ics.muted_until > NOW()
		       THEN ics.muted_until
		     ELSE NULL
		   END AS muted_until,
		   COALESCE(ics.note, '') AS state_note,
		   ics.updated_at AS state_updated_at
		 FROM issue_clusters ic
		 LEFT JOIN issue_cluster_states ics
		   ON ics.project_id = ic.project_id
		  AND ics.cluster_key = ic.key
		 WHERE ic.project_id = $1
		 ORDER BY ic.last_seen_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusters := make([]IssueCluster, 0)
	for rows.Next() {
		var cluster IssueCluster
		if err := rows.Scan(
			&cluster.ProjectID,
			&cluster.Key,
			&cluster.Symptom,
			&cluster.SessionCount,
			&cluster.UserCount,
			&cluster.RepresentativeSessionID,
			&cluster.Confidence,
			&cluster.LastSeenAt,
			&cluster.CreatedAt,
			&cluster.State,
			&cluster.Assignee,
			&cluster.MutedUntil,
			&cluster.StateNote,
			&cluster.StateUpdatedAt,
		); err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return clusters, nil
}

func (p *Postgres) UpsertIssueClusterState(
	ctx context.Context,
	projectID string,
	clusterKey string,
	state string,
	assignee string,
	mutedUntil *time.Time,
	note string,
) (IssueClusterState, error) {
	projectID = normalizeProjectID(projectID)
	clusterKey = strings.TrimSpace(clusterKey)
	if clusterKey == "" {
		return IssueClusterState{}, fmt.Errorf("clusterKey is required")
	}

	state = normalizeIssueState(state)
	assignee = strings.TrimSpace(assignee)
	note = strings.TrimSpace(note)

	if state != "muted" {
		mutedUntil = nil
	}

	stored := IssueClusterState{}
	err := p.pool.QueryRow(
		ctx,
		`INSERT INTO issue_cluster_states (
		   project_id, cluster_key, state, assignee, muted_until, note
		 )
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (project_id, cluster_key) DO UPDATE
		 SET state = EXCLUDED.state,
		     assignee = EXCLUDED.assignee,
		     muted_until = EXCLUDED.muted_until,
		     note = EXCLUDED.note,
		     updated_at = NOW()
		 RETURNING project_id, cluster_key, state, assignee, muted_until, note, created_at, updated_at`,
		projectID,
		clusterKey,
		state,
		assignee,
		mutedUntil,
		note,
	).Scan(
		&stored.ProjectID,
		&stored.ClusterKey,
		&stored.State,
		&stored.Assignee,
		&stored.MutedUntil,
		&stored.Note,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	)
	if err != nil {
		return IssueClusterState{}, err
	}

	return stored, nil
}

func (p *Postgres) IssueClusterExists(
	ctx context.Context,
	projectID string,
	clusterKey string,
) (bool, error) {
	projectID = normalizeProjectID(projectID)
	clusterKey = strings.TrimSpace(clusterKey)
	if clusterKey == "" {
		return false, nil
	}

	exists := false
	err := p.pool.QueryRow(
		ctx,
		`SELECT EXISTS (
		   SELECT 1
		   FROM issue_clusters
		   WHERE project_id = $1
		     AND key = $2
		 )`,
		projectID,
		clusterKey,
	).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (p *Postgres) LastIssueAlertAt(
	ctx context.Context,
	projectID string,
	clusterKey string,
	alertType string,
) (*time.Time, error) {
	projectID = normalizeProjectID(projectID)
	clusterKey = strings.TrimSpace(clusterKey)
	alertType = strings.TrimSpace(alertType)
	if clusterKey == "" || alertType == "" {
		return nil, nil
	}

	var sentAt time.Time
	err := p.pool.QueryRow(
		ctx,
		`SELECT sent_at
		 FROM issue_alert_events
		 WHERE project_id = $1
		   AND cluster_key = $2
		   AND alert_type = $3
		 ORDER BY sent_at DESC
		 LIMIT 1`,
		projectID,
		clusterKey,
		alertType,
	).Scan(&sentAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &sentAt, nil
}

func (p *Postgres) RecordIssueAlert(
	ctx context.Context,
	projectID string,
	clusterKey string,
	alertType string,
	payload any,
	sentAt time.Time,
) error {
	projectID = normalizeProjectID(projectID)
	clusterKey = strings.TrimSpace(clusterKey)
	alertType = strings.TrimSpace(alertType)
	if clusterKey == "" || alertType == "" {
		return fmt.Errorf("clusterKey and alertType are required")
	}
	if sentAt.IsZero() {
		sentAt = time.Now().UTC()
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(
		ctx,
		`INSERT INTO issue_alert_events (id, project_id, cluster_key, alert_type, payload, sent_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		"alert_"+uuid.NewString(),
		projectID,
		clusterKey,
		alertType,
		string(payloadJSON),
		sentAt,
	)
	return err
}

func (p *Postgres) ListIssueClusterSessions(
	ctx context.Context,
	projectID string,
	clusterKey string,
	reportStatus string,
	minConfidence float64,
	limit int,
) ([]IssueClusterSession, error) {
	projectID = normalizeProjectID(projectID)
	clusterKey = strings.TrimSpace(clusterKey)
	if clusterKey == "" {
		return []IssueClusterSession{}, nil
	}
	reportStatus = normalizeReportStatusFilter(reportStatus)
	if minConfidence < 0 {
		minConfidence = 0
	}
	if minConfidence > 1 {
		minConfidence = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := p.pool.Query(
		ctx,
		`SELECT
		   s.id,
		   s.project_id,
		   s.site,
		   s.route,
		   s.started_at,
		   s.duration_ms,
		   MAX(em.observed_at) AS last_observed_at,
		   COUNT(*)::int AS marker_count,
		   COALESCE(src.status, 'pending') AS report_status,
		   COALESCE(src.confidence, 0) AS report_confidence,
		   COALESCE(src.symptom, '') AS report_symptom
		 FROM error_markers em
		 JOIN sessions s ON s.id = em.session_id
		 LEFT JOIN session_report_cards src
		   ON src.project_id = s.project_id
		  AND src.session_id = s.id
		 WHERE s.project_id = $1
		   AND em.cluster_key = $2
		   AND ($3::text = '' OR COALESCE(src.status, 'pending') = $3::text)
		   AND COALESCE(src.confidence, 0) >= $4
		 GROUP BY
		   s.id,
		   s.project_id,
		   s.site,
		   s.route,
		   s.started_at,
		   s.duration_ms,
		   src.status,
		   src.confidence,
		   src.symptom
		 ORDER BY MAX(em.observed_at) DESC
		 LIMIT $5`,
		projectID,
		clusterKey,
		reportStatus,
		minConfidence,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]IssueClusterSession, 0)
	for rows.Next() {
		var session IssueClusterSession
		if err := rows.Scan(
			&session.SessionID,
			&session.ProjectID,
			&session.Site,
			&session.Route,
			&session.StartedAt,
			&session.DurationMs,
			&session.LastObservedAt,
			&session.MarkerCount,
			&session.ReportStatus,
			&session.ReportConfidence,
			&session.ReportSymptom,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return sessions, nil
}

func (p *Postgres) ListIssueKindStats(ctx context.Context, projectID string, lookback time.Duration) ([]IssueKindStat, error) {
	projectID = normalizeProjectID(projectID)
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	lookbackSeconds := int(lookback.Seconds())

	rows, err := p.pool.Query(
		ctx,
		`SELECT
		   em.kind,
		   COUNT(*)::int AS marker_count,
		   COUNT(DISTINCT em.session_id)::int AS session_count,
		   COUNT(DISTINCT em.cluster_key)::int AS cluster_count,
		   MAX(em.observed_at) AS last_seen_at
		 FROM error_markers em
		 JOIN sessions s ON s.id = em.session_id
		 WHERE s.project_id = $1
		   AND em.observed_at >= NOW() - ($2::int * interval '1 second')
		 GROUP BY em.kind
		 ORDER BY marker_count DESC, last_seen_at DESC`,
		projectID,
		lookbackSeconds,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]IssueKindStat, 0)
	for rows.Next() {
		var stat IssueKindStat
		if err := rows.Scan(
			&stat.Kind,
			&stat.MarkerCount,
			&stat.SessionCount,
			&stat.ClusterCount,
			&stat.LastSeenAt,
		); err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return stats, nil
}

func (p *Postgres) GetSession(ctx context.Context, projectID, id string) (Session, error) {
	projectID = normalizeProjectID(projectID)

	session := Session{}
	err := p.pool.QueryRow(
		ctx,
		`SELECT id, project_id, site, route, started_at, duration_ms, events_object_key, created_at
		 FROM sessions
		 WHERE id = $1
		   AND project_id = $2`,
		id,
		projectID,
	).Scan(
		&session.ID,
		&session.ProjectID,
		&session.Site,
		&session.Route,
		&session.StartedAt,
		&session.DurationMs,
		&session.EventsObjectKey,
		&session.CreatedAt,
	)
	if err != nil {
		return Session{}, err
	}

	rows, err := p.pool.Query(
		ctx,
		`SELECT id, session_id, cluster_key, label, replay_offset_ms, kind, observed_at
		 FROM error_markers
		 WHERE session_id = $1
		 ORDER BY replay_offset_ms ASC`,
		id,
	)
	if err != nil {
		return Session{}, err
	}
	defer rows.Close()

	session.Markers = make([]ErrorMark, 0)
	for rows.Next() {
		var marker ErrorMark
		if err := rows.Scan(
			&marker.ID,
			&marker.SessionID,
			&marker.ClusterKey,
			&marker.Label,
			&marker.ReplayOffsetMs,
			&marker.Kind,
			&marker.ObservedAt,
		); err != nil {
			return Session{}, err
		}
		session.Markers = append(session.Markers, marker)
	}

	if rows.Err() != nil {
		return Session{}, rows.Err()
	}

	artifactRows, err := p.pool.Query(
		ctx,
		`SELECT id, project_id, session_id, artifact_type, artifact_key, trigger_kind, status, marker_windows, generated_at, created_at, updated_at
		 FROM session_artifacts
		 WHERE project_id = $1
		   AND session_id = $2
		 ORDER BY generated_at DESC`,
		projectID,
		id,
	)
	if err != nil {
		return Session{}, err
	}
	defer artifactRows.Close()

	session.Artifacts = make([]SessionArtifact, 0)
	for artifactRows.Next() {
		var (
			artifact    SessionArtifact
			windowsJSON []byte
		)
		if err := artifactRows.Scan(
			&artifact.ID,
			&artifact.ProjectID,
			&artifact.SessionID,
			&artifact.ArtifactType,
			&artifact.ArtifactKey,
			&artifact.TriggerKind,
			&artifact.Status,
			&windowsJSON,
			&artifact.GeneratedAt,
			&artifact.CreatedAt,
			&artifact.UpdatedAt,
		); err != nil {
			return Session{}, err
		}

		artifact.Windows = make([]ArtifactWindow, 0)
		if len(windowsJSON) > 0 {
			if err := json.Unmarshal(windowsJSON, &artifact.Windows); err != nil {
				return Session{}, err
			}
		}

		session.Artifacts = append(session.Artifacts, artifact)
	}

	if artifactRows.Err() != nil {
		return Session{}, artifactRows.Err()
	}

	report := SessionReportCard{}
	reportErr := p.pool.QueryRow(
		ctx,
		`SELECT id, project_id, session_id, status, symptom, technical_root_cause, suggested_fix, text_summary, visual_summary, confidence, generated_at, created_at, updated_at
		 FROM session_report_cards
		 WHERE project_id = $1
		   AND session_id = $2`,
		projectID,
		id,
	).Scan(
		&report.ID,
		&report.ProjectID,
		&report.SessionID,
		&report.Status,
		&report.Symptom,
		&report.TechnicalRootCause,
		&report.SuggestedFix,
		&report.TextSummary,
		&report.VisualSummary,
		&report.Confidence,
		&report.GeneratedAt,
		&report.CreatedAt,
		&report.UpdatedAt,
	)
	if reportErr != nil && !errors.Is(reportErr, pgx.ErrNoRows) {
		return Session{}, reportErr
	}
	if reportErr == nil {
		session.ReportCard = &report
	}

	return session, nil
}

func (p *Postgres) CleanupExpiredData(ctx context.Context, projectID string, retentionDays int) (CleanupResult, error) {
	if retentionDays < 1 {
		return CleanupResult{}, fmt.Errorf("retentionDays must be >= 1")
	}
	projectID = normalizeProjectID(projectID)

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback(ctx)

	var (
		deletedSessions     int
		deletedEventObject  []string
		deletedArtifactKeys []string
	)
	err = tx.QueryRow(
		ctx,
		`WITH target_sessions AS (
		   SELECT id, events_object_key
		   FROM sessions
		   WHERE created_at < NOW() - ($1::text || ' days')::interval
		     AND project_id = $2
		 ),
		 target_artifacts AS (
		   SELECT DISTINCT sa.artifact_key
		   FROM session_artifacts sa
		   JOIN target_sessions ts ON ts.id = sa.session_id
		   WHERE sa.project_id = $2
		 ),
		 deleted_sessions AS (
		   DELETE FROM sessions s
		   USING target_sessions ts
		   WHERE s.id = ts.id
		   RETURNING ts.events_object_key
		 )
		 SELECT
		   (SELECT COUNT(*) FROM deleted_sessions),
		   COALESCE((SELECT array_agg(events_object_key) FROM deleted_sessions), '{}'::text[]),
		   COALESCE((SELECT array_agg(artifact_key) FROM target_artifacts), '{}'::text[])`,
		retentionDays,
		projectID,
	).Scan(&deletedSessions, &deletedEventObject, &deletedArtifactKeys)
	if err != nil {
		return CleanupResult{}, err
	}

	commandTag, err := tx.Exec(
		ctx,
		`DELETE FROM issue_clusters ic
		 WHERE ic.project_id = $1
		   AND NOT EXISTS (
		   SELECT 1
		   FROM error_markers em
		   JOIN sessions s ON s.id = em.session_id
		   WHERE em.cluster_key = ic.key
		     AND s.project_id = ic.project_id
		 )`,
		projectID,
	)
	if err != nil {
		return CleanupResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CleanupResult{}, err
	}

	return CleanupResult{
		DeletedSessions:           deletedSessions,
		DeletedIssueClusters:      int(commandTag.RowsAffected()),
		DeletedEventObjects:       len(deletedEventObject),
		DeletedArtifactObjects:    len(deletedArtifactKeys),
		DeletedEventObjectKeys:    deletedEventObject,
		DeletedArtifactObjectKeys: deletedArtifactKeys,
		RetentionDays:             retentionDays,
	}, nil
}

func (p *Postgres) ResolveProjectIDByAPIKey(ctx context.Context, rawKey string) (string, error) {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		return "", pgx.ErrNoRows
	}

	hash := hashAPIKey(key)
	projectID := ""

	err := p.pool.QueryRow(
		ctx,
		`UPDATE project_api_keys
		 SET last_used_at = NOW()
		 WHERE key_hash = $1
		   AND status = 'active'
		 RETURNING project_id`,
		hash,
	).Scan(&projectID)
	if err != nil {
		return "", err
	}

	return projectID, nil
}

func (p *Postgres) CreateProjectWithAPIKey(
	ctx context.Context,
	name,
	site,
	label,
	rawKey string,
) (Project, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback(ctx)

	projectID := "proj_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	project := Project{}

	err = tx.QueryRow(
		ctx,
		`INSERT INTO projects (id, name, site)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, site, created_at`,
		projectID,
		strings.TrimSpace(name),
		strings.TrimSpace(site),
	).Scan(&project.ID, &project.Name, &project.Site, &project.CreatedAt)
	if err != nil {
		return Project{}, err
	}

	keyID := "key_" + uuid.NewString()
	_, err = tx.Exec(
		ctx,
		`INSERT INTO project_api_keys (id, project_id, key_hash, label, status)
		 VALUES ($1, $2, $3, $4, 'active')`,
		keyID,
		projectID,
		hashAPIKey(strings.TrimSpace(rawKey)),
		strings.TrimSpace(label),
	)
	if err != nil {
		return Project{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Project{}, err
	}

	return project, nil
}

func (p *Postgres) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := p.pool.Query(
		ctx,
		`SELECT id, name, site, created_at
		 FROM projects
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Site, &project.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return projects, nil
}

func (p *Postgres) CreateAPIKeyForProject(
	ctx context.Context,
	projectID,
	label,
	rawKey string,
) (ProjectAPIKey, error) {
	projectID = normalizeProjectID(projectID)
	keyID := "key_" + uuid.NewString()
	stored := ProjectAPIKey{}

	err := p.pool.QueryRow(
		ctx,
		`INSERT INTO project_api_keys (id, project_id, key_hash, label, status)
		 VALUES ($1, $2, $3, $4, 'active')
		 RETURNING id, project_id, label, status, created_at, last_used_at`,
		keyID,
		projectID,
		hashAPIKey(strings.TrimSpace(rawKey)),
		strings.TrimSpace(label),
	).Scan(
		&stored.ID,
		&stored.ProjectID,
		&stored.Label,
		&stored.Status,
		&stored.CreatedAt,
		&stored.LastUsedAt,
	)
	if err != nil {
		return ProjectAPIKey{}, err
	}

	return stored, nil
}

func (p *Postgres) ListProjectAPIKeys(ctx context.Context, projectID string) ([]ProjectAPIKey, error) {
	projectID = normalizeProjectID(projectID)
	rows, err := p.pool.Query(
		ctx,
		`SELECT id, project_id, label, status, created_at, last_used_at
		 FROM project_api_keys
		 WHERE project_id = $1
		 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]ProjectAPIKey, 0)
	for rows.Next() {
		var apiKey ProjectAPIKey
		if err := rows.Scan(
			&apiKey.ID,
			&apiKey.ProjectID,
			&apiKey.Label,
			&apiKey.Status,
			&apiKey.CreatedAt,
			&apiKey.LastUsedAt,
		); err != nil {
			return nil, err
		}
		keys = append(keys, apiKey)
	}

	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return keys, nil
}

func (p *Postgres) UpsertSessionArtifact(
	ctx context.Context,
	projectID string,
	artifactType string,
	sessionID string,
	artifactKey string,
	triggerKind string,
	status string,
	windows []ArtifactWindow,
	generatedAt time.Time,
) (SessionArtifact, error) {
	projectID = normalizeProjectID(projectID)
	sessionID = strings.TrimSpace(sessionID)
	artifactKey = strings.TrimSpace(artifactKey)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "ready"
	}
	if sessionID == "" {
		return SessionArtifact{}, fmt.Errorf("sessionID is required")
	}
	if artifactKey == "" && status == "ready" {
		return SessionArtifact{}, fmt.Errorf("artifactKey is required")
	}

	artifactType = strings.TrimSpace(artifactType)
	if artifactType == "" {
		artifactType = "analysis_json"
	}

	triggerKind = strings.TrimSpace(triggerKind)
	if triggerKind == "" {
		triggerKind = "ui_no_effect"
	}

	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	windowsJSON, err := json.Marshal(windows)
	if err != nil {
		return SessionArtifact{}, err
	}

	artifactID := "art_" + uuid.NewString()
	stored := SessionArtifact{}
	scanErr := p.pool.QueryRow(
		ctx,
		`INSERT INTO session_artifacts (
		   id, project_id, session_id, artifact_type, artifact_key, trigger_kind, status, marker_windows, generated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
		 ON CONFLICT (project_id, session_id, artifact_type) DO UPDATE
		 SET artifact_key = EXCLUDED.artifact_key,
		     trigger_kind = EXCLUDED.trigger_kind,
		     status = EXCLUDED.status,
		     marker_windows = EXCLUDED.marker_windows,
		     generated_at = EXCLUDED.generated_at,
		     updated_at = NOW()
		 RETURNING id, project_id, session_id, artifact_type, artifact_key, trigger_kind, status, marker_windows, generated_at, created_at, updated_at`,
		artifactID,
		projectID,
		sessionID,
		artifactType,
		artifactKey,
		triggerKind,
		status,
		string(windowsJSON),
		generatedAt,
	).Scan(
		&stored.ID,
		&stored.ProjectID,
		&stored.SessionID,
		&stored.ArtifactType,
		&stored.ArtifactKey,
		&stored.TriggerKind,
		&stored.Status,
		&windowsJSON,
		&stored.GeneratedAt,
		&stored.CreatedAt,
		&stored.UpdatedAt,
	)
	if scanErr != nil {
		return SessionArtifact{}, scanErr
	}

	stored.Windows = make([]ArtifactWindow, 0)
	if len(windowsJSON) > 0 {
		if err := json.Unmarshal(windowsJSON, &stored.Windows); err != nil {
			return SessionArtifact{}, err
		}
	}

	return stored, nil
}

func (p *Postgres) UpsertSessionReportCard(
	ctx context.Context,
	projectID string,
	sessionID string,
	status string,
	symptom string,
	technicalRootCause string,
	suggestedFix string,
	textSummary string,
	visualSummary string,
	confidence float64,
	generatedAt time.Time,
) (SessionReportCard, error) {
	projectID = normalizeProjectID(projectID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionReportCard{}, fmt.Errorf("sessionID is required")
	}

	status = normalizeReportStatus(status)
	symptom = strings.TrimSpace(symptom)
	technicalRootCause = strings.TrimSpace(technicalRootCause)
	suggestedFix = strings.TrimSpace(suggestedFix)
	textSummary = strings.TrimSpace(textSummary)
	visualSummary = strings.TrimSpace(visualSummary)
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	reportID := "report_" + uuid.NewString()
	report := SessionReportCard{}
	err := p.pool.QueryRow(
		ctx,
		`INSERT INTO session_report_cards (
		   id, project_id, session_id, status, symptom, technical_root_cause, suggested_fix, text_summary, visual_summary, confidence, generated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (project_id, session_id) DO UPDATE
		 SET status = EXCLUDED.status,
		     symptom = EXCLUDED.symptom,
		     technical_root_cause = EXCLUDED.technical_root_cause,
		     suggested_fix = EXCLUDED.suggested_fix,
		     text_summary = EXCLUDED.text_summary,
		     visual_summary = EXCLUDED.visual_summary,
		     confidence = EXCLUDED.confidence,
		     generated_at = EXCLUDED.generated_at,
		     updated_at = NOW()
		 RETURNING id, project_id, session_id, status, symptom, technical_root_cause, suggested_fix, text_summary, visual_summary, confidence, generated_at, created_at, updated_at`,
		reportID,
		projectID,
		sessionID,
		status,
		symptom,
		technicalRootCause,
		suggestedFix,
		textSummary,
		visualSummary,
		confidence,
		generatedAt,
	).Scan(
		&report.ID,
		&report.ProjectID,
		&report.SessionID,
		&report.Status,
		&report.Symptom,
		&report.TechnicalRootCause,
		&report.SuggestedFix,
		&report.TextSummary,
		&report.VisualSummary,
		&report.Confidence,
		&report.GeneratedAt,
		&report.CreatedAt,
		&report.UpdatedAt,
	)
	if err != nil {
		return SessionReportCard{}, err
	}

	return report, nil
}

func normalizeMarkerKind(kind string) string {
	trimmed := strings.TrimSpace(kind)
	switch trimmed {
	case "validation_failed", "api_error", "js_exception", "ui_no_effect":
		return trimmed
	default:
		return "ui_no_effect"
	}
}

func normalizeReportStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pending", "ready", "failed", "discarded":
		return strings.TrimSpace(status)
	default:
		return "pending"
	}
}

func normalizeReportStatusFilter(status string) string {
	switch strings.TrimSpace(status) {
	case "", "pending", "ready", "failed", "discarded":
		return strings.TrimSpace(status)
	default:
		return ""
	}
}

func normalizeIssueState(state string) string {
	switch strings.TrimSpace(state) {
	case "open", "acknowledged", "resolved", "muted":
		return strings.TrimSpace(state)
	default:
		return "open"
	}
}

func deriveClusterKey(route, kind, clusterHint, label string) string {
	normalizedKind := normalizeMarkerKind(kind)
	normalizedRoute := normalizeRouteForCluster(route)
	normalizedHint := normalizeTextForCluster(clusterHint)
	normalizedLabel := normalizeTextForCluster(label)

	if normalizedHint == "" || normalizedHint == "unknown" {
		normalizedHint = normalizedLabel
	}
	if normalizedLabel == "" || normalizedLabel == "unknown" {
		normalizedLabel = normalizedHint
	}
	if normalizedHint == "" {
		normalizedHint = "unknown"
	}
	if normalizedLabel == "" {
		normalizedLabel = "unknown"
	}

	base := strings.Join([]string{
		normalizedKind,
		normalizedRoute,
		normalizedHint,
		normalizedLabel,
	}, "|")
	hash := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%s:%s", normalizedKind, hex.EncodeToString(hash[:8]))
}

func normalizeTextForCluster(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return "unknown"
	}

	value = clusterEmailRegex.ReplaceAllString(value, "<email>")
	value = clusterUUIDRegex.ReplaceAllString(value, "<uuid>")
	value = clusterURLRegex.ReplaceAllStringFunc(value, normalizeURLForCluster)
	value = clusterStatusCodeRegex.ReplaceAllString(value, "$1xx")
	value = clusterHexRegex.ReplaceAllString(value, "<hex>")
	value = clusterNumberRegex.ReplaceAllString(value, "<num>")
	value = clusterSpaceRegex.ReplaceAllString(value, " ")
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func normalizeRouteForCluster(route string) string {
	trimmed := strings.TrimSpace(strings.ToLower(route))
	if trimmed == "" {
		return "/unknown"
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		parsed, err := url.Parse(trimmed)
		if err == nil && parsed.Path != "" {
			return normalizePathForCluster(parsed.Path)
		}
	}

	return normalizePathForCluster(trimmed)
}

func normalizeURLForCluster(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return normalizeTextForClusterToken(raw)
	}

	host := strings.ToLower(parsed.Hostname())
	normalizedPath := normalizePathForCluster(parsed.Path)
	if normalizedPath == "" {
		normalizedPath = "/"
	}
	if host == "" {
		return normalizedPath
	}

	return host + normalizedPath
}

func normalizePathForCluster(path string) string {
	value := strings.TrimSpace(strings.ToLower(path))
	if value == "" {
		return "/unknown"
	}

	if idx := strings.Index(value, "?"); idx >= 0 {
		value = value[:idx]
	}
	if idx := strings.Index(value, "#"); idx >= 0 {
		value = value[:idx]
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}

	parts := strings.Split(value, "/")
	for index, part := range parts {
		if part == "" {
			continue
		}

		normalized := normalizeTextForClusterToken(part)
		switch {
		case clusterUUIDRegex.MatchString(normalized):
			parts[index] = ":id"
		case clusterHexRegex.MatchString(normalized):
			parts[index] = ":id"
		case clusterNumberRegex.MatchString(normalized):
			parts[index] = ":id"
		default:
			parts[index] = normalized
		}
	}

	normalizedPath := strings.Join(parts, "/")
	if normalizedPath == "" {
		return "/unknown"
	}
	return normalizedPath
}

func normalizeTextForClusterToken(value string) string {
	token := strings.ToLower(strings.TrimSpace(value))
	token = clusterTokenRegex.ReplaceAllString(token, "-")
	token = strings.Trim(token, "-")
	if token == "" {
		return "unknown"
	}
	return token
}

func hashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func normalizeProjectID(projectID string) string {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return "proj_default"
	}
	return trimmed
}
