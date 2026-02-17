CREATE TABLE IF NOT EXISTS issue_feedback_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  cluster_key TEXT NOT NULL,
  session_id TEXT,
  feedback_kind TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_by TEXT NOT NULL DEFAULT 'dashboard',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_feedback_events_kind_check'
  ) THEN
    ALTER TABLE issue_feedback_events
      ADD CONSTRAINT issue_feedback_events_kind_check
      CHECK (
        feedback_kind IN (
          'false_positive',
          'true_positive',
          'invalid',
          'suppressed',
          'unsuppressed',
          'merge',
          'split'
        )
      );
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS issue_feedback_events_project_cluster_created_idx
  ON issue_feedback_events (project_id, cluster_key, created_at DESC);

CREATE INDEX IF NOT EXISTS issue_feedback_events_kind_created_idx
  ON issue_feedback_events (feedback_kind, created_at DESC);
