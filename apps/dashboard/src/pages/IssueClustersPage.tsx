import { useState } from "react";
import { Link } from "react-router-dom";
import {
  useCleanupDataMutation,
  useGetIssuesQuery,
  usePromoteIssuesMutation,
} from "../features/reporting/reportingApi";

export function IssueClustersPage() {
  const {
    data: clusters = [],
    isLoading,
    isFetching,
    isError,
  } = useGetIssuesQuery();
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
      `Cleanup: ${result.deletedSessions} sessions, ${result.deletedIssueClusters} clusters, ${result.deletedEventObjects} event objects.`,
    );
  };

  return (
    <section>
      <h1>Promoted Issue Clusters</h1>
      <p>
        Only repeated failures are shown here. A cluster appears when it passes confidence and
        recurrence thresholds.
      </p>
      <div className="issue-actions">
        <button type="button" onClick={handlePromote} disabled={isPromoting}>
          {isPromoting ? "Promoting..." : "Recompute Clusters"}
        </button>
        <button type="button" onClick={handleCleanup} disabled={isCleaning}>
          {isCleaning ? "Cleaning..." : "Run Retention Cleanup"}
        </button>
        {isFetching && <span>Refreshing...</span>}
      </div>
      {maintenanceMessage && <p>{maintenanceMessage}</p>}
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
