CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  site TEXT NOT NULL,
  route TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL,
  duration_ms INTEGER NOT NULL,
  events_object_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS error_markers (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  cluster_key TEXT NOT NULL,
  label TEXT NOT NULL,
  replay_offset_ms INTEGER NOT NULL,
  kind TEXT NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS error_markers_session_id_idx ON error_markers (session_id);
CREATE INDEX IF NOT EXISTS error_markers_cluster_key_idx ON error_markers (cluster_key);

CREATE TABLE IF NOT EXISTS issue_clusters (
  key TEXT PRIMARY KEY,
  symptom TEXT NOT NULL,
  session_count INTEGER NOT NULL,
  user_count INTEGER NOT NULL,
  confidence DOUBLE PRECISION NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS issue_clusters_last_seen_at_idx ON issue_clusters (last_seen_at DESC);
