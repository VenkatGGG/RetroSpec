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
  const {
    data: clusters = [],
    isLoading,
    isFetching,
    isError,
  } = useGetIssuesQuery();
  const { data: issueStats } = useGetIssueStatsQuery(lookbackHours);
  const [promoteIssues, { isLoading: isPromoting }] = usePromoteIssuesMutation();
  const [cleanupData, { isLoading: isCleaning }] = useCleanupDataMutation();
  const [maintenanceMessage, setMaintenanceMessage] = useState<string>("");

  const handlePromote = async () => {
    await promoteIssues();
    setMaintenanceMessage("");
  };

  const handleCleanup = async () => {
    const result = await cleanupData().unwrap();
    setMaintenanceMessage(
      `Cleanup: ${result.deletedSessions} sessions, ${result.deletedIssueClusters} clusters, ${result.deletedEventObjects} event objects, ${result.deletedArtifactObjects} replay artifacts.`,
    );
  };

  return (
    <section>
      <h1>Promoted Issue Clusters</h1>
      <p>
        Only repeated failures are shown here. A cluster appears when it passes confidence and
        recurrence thresholds.
      </p>
      <p>
        <Link to="/admin">Open admin controls</Link>
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
        <button type="button" onClick={handlePromote} disabled={isPromoting}>
          {isPromoting ? "Promoting..." : "Recompute Clusters"}
        </button>
        <button type="button" onClick={handleCleanup} disabled={isCleaning}>
          {isCleaning ? "Cleaning..." : "Run Retention Cleanup"}
        </button>
        {isFetching && <span>Refreshing...</span>}
      </div>
      {maintenanceMessage && <p>{maintenanceMessage}</p>}
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
      {isError && <p>Unable to load issues. Check API connectivity.</p>}
      {!isLoading && !isError && clusters.length === 0 && (
        <p>No promoted clusters yet. Trigger promotion after ingesting sessions.</p>
      )}

      <div className="cluster-grid">
        {clusters.map((cluster) => (
          <article key={cluster.key} className="cluster-card">
            <h2>{cluster.symptom}</h2>
            <p>
              <strong>Sessions:</strong> {cluster.sessionCount} | <strong>Users:</strong>{" "}
              {cluster.userCount}
            </p>
            <p>
              <strong>Cluster Key:</strong> <code>{cluster.key}</code>
            </p>
            <p>
              <strong>Last Seen:</strong> {new Date(cluster.lastSeenAt).toLocaleString()}
            </p>
            <p>
              <strong>Confidence:</strong> {cluster.confidence.toFixed(2)}
            </p>
            <Link to={`/sessions/${cluster.representativeSessionId}`}>Open representative replay</Link>
          </article>
        ))}
      </div>
    </section>
  );
}
