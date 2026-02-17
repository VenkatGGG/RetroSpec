CREATE TABLE IF NOT EXISTS issue_alert_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  cluster_key TEXT NOT NULL,
  alert_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'issue_alert_events_issue_cluster_fkey'
  ) THEN
    ALTER TABLE issue_alert_events
      ADD CONSTRAINT issue_alert_events_issue_cluster_fkey
      FOREIGN KEY (project_id, cluster_key)
      REFERENCES issue_clusters(project_id, key)
      ON DELETE CASCADE;
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS issue_alert_events_lookup_idx
  ON issue_alert_events (project_id, cluster_key, alert_type, sent_at DESC);

CREATE INDEX IF NOT EXISTS issue_alert_events_sent_at_idx
  ON issue_alert_events (sent_at DESC);
