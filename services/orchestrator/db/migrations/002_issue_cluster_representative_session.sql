ALTER TABLE issue_clusters
ADD COLUMN IF NOT EXISTS representative_session_id TEXT;

UPDATE issue_clusters ic
SET representative_session_id = latest.session_id
FROM (
  SELECT em.cluster_key, em.session_id
  FROM error_markers em
  JOIN (
    SELECT cluster_key, MAX(observed_at) AS max_observed_at
    FROM error_markers
    WHERE cluster_key <> ''
    GROUP BY cluster_key
  ) ranked ON ranked.cluster_key = em.cluster_key AND ranked.max_observed_at = em.observed_at
) latest
WHERE ic.key = latest.cluster_key
  AND (ic.representative_session_id IS NULL OR ic.representative_session_id = '');

UPDATE issue_clusters
SET representative_session_id = ''
WHERE representative_session_id IS NULL;

ALTER TABLE issue_clusters
ALTER COLUMN representative_session_id SET NOT NULL;
