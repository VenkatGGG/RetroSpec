CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  site TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO projects (id, name, site)
VALUES ('proj_default', 'Default Project', 'default.local')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS project_api_keys (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key_hash TEXT NOT NULL UNIQUE,
  label TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS project_api_keys_project_id_idx ON project_api_keys (project_id);
CREATE INDEX IF NOT EXISTS project_api_keys_status_idx ON project_api_keys (status);

ALTER TABLE sessions
ADD COLUMN IF NOT EXISTS project_id TEXT;

UPDATE sessions
SET project_id = 'proj_default'
WHERE project_id IS NULL OR project_id = '';

ALTER TABLE sessions
ALTER COLUMN project_id SET NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'sessions_project_id_fkey'
  ) THEN
    ALTER TABLE sessions
    ADD CONSTRAINT sessions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT;
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS sessions_project_id_idx ON sessions (project_id);

ALTER TABLE issue_clusters
ADD COLUMN IF NOT EXISTS project_id TEXT;

UPDATE issue_clusters
SET project_id = 'proj_default'
WHERE project_id IS NULL OR project_id = '';

ALTER TABLE issue_clusters
ALTER COLUMN project_id SET NOT NULL;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_clusters_pkey'
  ) THEN
    ALTER TABLE issue_clusters
    DROP CONSTRAINT issue_clusters_pkey;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_clusters_project_key_pkey'
  ) THEN
    ALTER TABLE issue_clusters
    ADD CONSTRAINT issue_clusters_project_key_pkey PRIMARY KEY (project_id, key);
  END IF;
END
$$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_clusters_project_id_fkey'
  ) THEN
    ALTER TABLE issue_clusters
    ADD CONSTRAINT issue_clusters_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS issue_clusters_project_last_seen_idx ON issue_clusters (project_id, last_seen_at DESC);
