package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

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
			marker.ClusterKey,
			marker.Label,
			marker.ReplayOffsetMs,
			marker.Kind,
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
		`SELECT project_id, key, symptom, session_count, user_count, representative_session_id, confidence, last_seen_at, created_at
		 FROM issue_clusters
		 WHERE project_id = $1
		 ORDER BY last_seen_at DESC`,
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
		deletedSessions    int
		deletedEventObject []string
	)
	err = tx.QueryRow(
		ctx,
		`WITH deleted AS (
		   DELETE FROM sessions
		   WHERE created_at < NOW() - ($1::text || ' days')::interval
		     AND project_id = $2
		   RETURNING events_object_key
		 )
		 SELECT COUNT(*), COALESCE(array_agg(events_object_key), '{}'::text[]) FROM deleted`,
		retentionDays,
		projectID,
	).Scan(&deletedSessions, &deletedEventObject)
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
		DeletedSessions:        deletedSessions,
		DeletedIssueClusters:   int(commandTag.RowsAffected()),
		DeletedEventObjects:    len(deletedEventObject),
		DeletedEventObjectKeys: deletedEventObject,
		RetentionDays:          retentionDays,
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
