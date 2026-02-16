CREATE UNIQUE INDEX IF NOT EXISTS sessions_project_id_id_uidx
  ON sessions (project_id, id);

CREATE TABLE IF NOT EXISTS session_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  artifact_type TEXT NOT NULL,
  artifact_key TEXT NOT NULL,
  trigger_kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'ready',
  marker_windows JSONB NOT NULL DEFAULT '[]'::jsonb,
  generated_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (project_id, session_id, artifact_type)
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'session_artifacts_project_id_fkey'
  ) THEN
    ALTER TABLE session_artifacts
      ADD CONSTRAINT session_artifacts_project_id_fkey
      FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
  END IF;
END
$$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'session_artifacts_project_session_fkey'
  ) THEN
    ALTER TABLE session_artifacts
      ADD CONSTRAINT session_artifacts_project_session_fkey
      FOREIGN KEY (project_id, session_id) REFERENCES sessions(project_id, id) ON DELETE CASCADE;
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS session_artifacts_project_session_idx
  ON session_artifacts (project_id, session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS session_artifacts_generated_idx
  ON session_artifacts (generated_at DESC);
