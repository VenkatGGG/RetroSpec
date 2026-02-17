CREATE TABLE IF NOT EXISTS session_report_cards (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  symptom TEXT NOT NULL DEFAULT '',
  technical_root_cause TEXT NOT NULL DEFAULT '',
  suggested_fix TEXT NOT NULL DEFAULT '',
  text_summary TEXT NOT NULL DEFAULT '',
  visual_summary TEXT NOT NULL DEFAULT '',
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  generated_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (project_id, session_id)
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'session_report_cards_project_id_fkey'
  ) THEN
    ALTER TABLE session_report_cards
      ADD CONSTRAINT session_report_cards_project_id_fkey
      FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
  END IF;
END
$$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'session_report_cards_project_session_fkey'
  ) THEN
    ALTER TABLE session_report_cards
      ADD CONSTRAINT session_report_cards_project_session_fkey
      FOREIGN KEY (project_id, session_id) REFERENCES sessions(project_id, id) ON DELETE CASCADE;
  END IF;
END
$$;

CREATE INDEX IF NOT EXISTS session_report_cards_project_session_idx
  ON session_report_cards (project_id, session_id);

CREATE INDEX IF NOT EXISTS session_report_cards_generated_idx
  ON session_report_cards (generated_at DESC);
