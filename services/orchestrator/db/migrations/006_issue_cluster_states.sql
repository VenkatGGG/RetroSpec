CREATE TABLE IF NOT EXISTS issue_cluster_states (
  project_id TEXT NOT NULL,
  cluster_key TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'open',
  assignee TEXT NOT NULL DEFAULT '',
  muted_until TIMESTAMPTZ,
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (project_id, cluster_key)
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_cluster_states_issue_cluster_fkey'
  ) THEN
    ALTER TABLE issue_cluster_states
      ADD CONSTRAINT issue_cluster_states_issue_cluster_fkey
      FOREIGN KEY (project_id, cluster_key)
      REFERENCES issue_clusters(project_id, key)
      ON DELETE CASCADE;
  END IF;
END
$$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_cluster_states_state_check'
  ) THEN
    ALTER TABLE issue_cluster_states
      ADD CONSTRAINT issue_cluster_states_state_check
      CHECK (state IN ('open', 'acknowledged', 'resolved', 'muted'));
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS issue_cluster_states_project_state_idx
  ON issue_cluster_states (project_id, state, updated_at DESC);
