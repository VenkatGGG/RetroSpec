import { useState } from "react";
import { Link } from "react-router-dom";
import {
  useCleanupDataMutation,
  useGetIssuesQuery,
  useGetIssueStatsQuery,
  usePromoteIssuesMutation,
} from "../features/reporting/reportingApi";

export function IssueClustersPage() {
  const [lookbackHours, setLookbackHours] = useState(24);
  const [issueStateFilter, setIssueStateFilter] = useState<"" | "active">("active");
  const {
    data: clusters = [],
    isLoading,
    isFetching,
    isError,
  } = useGetIssuesQuery(issueStateFilter);
  const { data: issueStats } = useGetIssueStatsQuery(lookbackHours);
  const [promoteIssues, { isLoading: isPromoting }] = usePromoteIssuesMutation();
  const [cleanupData, { isLoading: isCleaning }] = useCleanupDataMutation();
  const [message, setMessage] = useState("");

  const handlePromote = async () => {
    try {
      await promoteIssues().unwrap();
      setMessage("Cluster promotion completed.");
    } catch {
      setMessage("Cluster promotion failed.");
    }
  };

  const handleCleanup = async () => {
    try {
      const result = await cleanupData().unwrap();
      setMessage(
        `Cleanup completed: ${result.deletedSessions} sessions, ${result.deletedEventObjects} event objects, ${result.deletedArtifactObjects} artifacts.`,
      );
    } catch {
      setMessage("Cleanup failed.");
    }
  };

  return (
    <section>
      <h1>Issue Clusters</h1>
      <p>
        This view shows recurring issues only. Clusters are promoted when similar failures are
        observed in at least two sessions.
      </p>
      <div className="issue-actions">
        <label htmlFor="stats-window-select">Stats Window</label>
        <select
          id="stats-window-select"
          value={lookbackHours}
          onChange={(event) => setLookbackHours(Number(event.target.value))}
        >
          <option value={24}>24h</option>
          <option value={72}>72h</option>
          <option value={168}>7d</option>
        </select>
        <label htmlFor="issue-state-filter-select">Issue State</label>
        <select
          id="issue-state-filter-select"
          value={issueStateFilter}
          onChange={(event) => setIssueStateFilter(event.target.value as "" | "active")}
        >
          <option value="active">active</option>
          <option value="">all</option>
        </select>
        <button type="button" onClick={handlePromote} disabled={isPromoting}>
          {isPromoting ? "Promoting..." : "Recompute Clusters"}
        </button>
        <button type="button" onClick={handleCleanup} disabled={isCleaning}>
          {isCleaning ? "Cleaning..." : "Run Retention Cleanup"}
        </button>
        {isFetching && <span>Refreshing...</span>}
      </div>
      {message && <p>{message}</p>}

      {issueStats && issueStats.stats.length > 0 && (
        <section className="stats-grid">
          {issueStats.stats.map((stat) => (
            <article className="stats-card" key={stat.kind}>
              <h3>{stat.kind}</h3>
              <p>
                <strong>Markers:</strong> {stat.markerCount}
              </p>
              <p>
                <strong>Sessions:</strong> {stat.sessionCount}
              </p>
              <p>
                <strong>Clusters:</strong> {stat.clusterCount}
              </p>
              <p>
                <strong>Last Seen:</strong> {new Date(stat.lastSeenAt).toLocaleString()}
              </p>
            </article>
          ))}
        </section>
      )}

      {isLoading && <p>Loading issue clusters...</p>}
      {isError && <p>Unable to load issue clusters.</p>}
      {!isLoading && !isError && clusters.length === 0 && (
        <p>No promoted clusters found yet.</p>
      )}

      {clusters.length > 0 && (
        <div className="cluster-grid">
          {clusters.map((cluster) => (
            <article className="cluster-card" key={cluster.key}>
              <h2>{cluster.key}</h2>
              <p>
                <strong>Symptom:</strong> {cluster.symptom || "Repeated session failure"}
              </p>
              <p>
                <strong>Sessions:</strong> {cluster.sessionCount}
              </p>
              <p>
                <strong>Last Seen:</strong> {new Date(cluster.lastSeenAt).toLocaleString()}
              </p>
              <p>
                <strong>Confidence:</strong>{" "}
                {(Math.max(0, Math.min(1, cluster.confidence)) * 100).toFixed(0)}%
              </p>
              <Link to={`/issues/${encodeURIComponent(cluster.key)}/sessions`}>
                Inspect Sessions
              </Link>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
