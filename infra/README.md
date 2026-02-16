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
```

## Stop

```bash
cd infra
docker compose down
```
