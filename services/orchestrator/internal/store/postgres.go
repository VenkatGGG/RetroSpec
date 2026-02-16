package store

import (
	"context"
	"fmt"
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

func (p *Postgres) Ingest(ctx context.Context, payload IngestPayload) (Session, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)

	sessionID := payload.Session.ID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	storedSession := Session{}
	err = tx.QueryRow(
		ctx,
		`INSERT INTO sessions (id, site, route, started_at, duration_ms, events_object_key)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE
		 SET site = EXCLUDED.site,
		     route = EXCLUDED.route,
		     started_at = EXCLUDED.started_at,
		     duration_ms = EXCLUDED.duration_ms,
		     events_object_key = EXCLUDED.events_object_key
		 RETURNING id, site, route, started_at, duration_ms, events_object_key, created_at`,
		sessionID,
		payload.Session.Site,
		payload.Session.Route,
		payload.Session.StartedAt,
		payload.Session.DurationMs,
		payload.Session.EventsObjectKey,
	).Scan(
		&storedSession.ID,
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

func (p *Postgres) PromoteClusters(ctx context.Context, minSessions int) (PromoteResult, error) {
	if minSessions < 1 {
		return PromoteResult{}, fmt.Errorf("minSessions must be >= 1")
	}

	rows, err := p.pool.Query(
		ctx,
		`WITH grouped AS (
		    SELECT
		      cluster_key,
		      MAX(label) AS symptom,
		      COUNT(DISTINCT session_id) AS session_count,
		      COUNT(DISTINCT session_id) AS user_count,
		      MAX(observed_at) AS last_seen_at
		    FROM error_markers
		    WHERE cluster_key <> ''
		    GROUP BY cluster_key
		)
		INSERT INTO issue_clusters (key, symptom, session_count, user_count, confidence, last_seen_at)
		SELECT
		  g.cluster_key,
		  g.symptom,
		  g.session_count,
		  g.user_count,
		  LEAST(1.0, g.session_count::float / ($1::float + 1.0)) AS confidence,
		  g.last_seen_at
		FROM grouped g
		WHERE g.session_count >= $1
		ON CONFLICT (key) DO UPDATE
		SET symptom = EXCLUDED.symptom,
		    session_count = EXCLUDED.session_count,
		    user_count = EXCLUDED.user_count,
		    confidence = EXCLUDED.confidence,
		    last_seen_at = EXCLUDED.last_seen_at
		RETURNING key, symptom, session_count, user_count, confidence, last_seen_at, created_at`,
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
			&cluster.Key,
			&cluster.Symptom,
			&cluster.SessionCount,
			&cluster.UserCount,
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

func (p *Postgres) ListIssueClusters(ctx context.Context) ([]IssueCluster, error) {
	rows, err := p.pool.Query(
		ctx,
		`SELECT key, symptom, session_count, user_count, confidence, last_seen_at, created_at
		 FROM issue_clusters
		 ORDER BY last_seen_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusters := make([]IssueCluster, 0)
	for rows.Next() {
		var cluster IssueCluster
		if err := rows.Scan(
			&cluster.Key,
			&cluster.Symptom,
			&cluster.SessionCount,
			&cluster.UserCount,
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

func (p *Postgres) GetSession(ctx context.Context, id string) (Session, error) {
	session := Session{}
	err := p.pool.QueryRow(
		ctx,
		`SELECT id, site, route, started_at, duration_ms, events_object_key, created_at
		 FROM sessions
		 WHERE id = $1`,
		id,
	).Scan(
		&session.ID,
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
