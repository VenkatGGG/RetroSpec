# Local Infra

Local dependencies for RetroSpec development:

- PostgreSQL (`localhost:5432`)
- Redis (`localhost:6379`)
- MinIO S3-compatible object storage (`localhost:9000`)

## Start

```bash
cd infra
docker compose up -d
```

## Apply migration

```bash
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/001_init.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/002_issue_cluster_representative_session.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/003_projects_and_project_api_keys.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/004_session_artifacts.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/005_session_report_cards.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/006_issue_cluster_states.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/007_issue_alert_events.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/008_error_markers_evidence.sql
psql postgresql://retrospec:retrospec@localhost:5432/retrospec -f ../services/orchestrator/db/migrations/009_issue_feedback_and_cluster_ops.sql
```

## Stop

```bash
cd infra
docker compose down
```
