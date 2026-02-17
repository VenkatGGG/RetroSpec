import { Link, useParams } from "react-router-dom";
import { useGetIssueSessionsQuery } from "../features/reporting/reportingApi";

export function IssueClusterSessionsPage() {
  const { clusterKey } = useParams();
  const decodedClusterKey = clusterKey ? decodeURIComponent(clusterKey) : "";
  const {
    data,
    isLoading,
    isError,
  } = useGetIssueSessionsQuery(
    { clusterKey: decodedClusterKey, limit: 50 },
    {
      skip: !decodedClusterKey,
      pollingInterval: decodedClusterKey ? 15_000 : 0,
      refetchOnMountOrArgChange: true,
    },
  );

  return (
    <section>
      <div className="session-heading">
        <div>
          <h1>Cluster Sessions</h1>
          <p>
            <strong>Cluster Key:</strong> <code>{decodedClusterKey || "-"}</code>
          </p>
        </div>
        <Link to="/">Back to issue clusters</Link>
      </div>
      {isLoading && <p>Loading sessions for this cluster...</p>}
      {isError && <p>Unable to load cluster sessions.</p>}
      {data && data.sessions.length === 0 && (
        <p>No sessions currently mapped to this cluster key.</p>
      )}
      {data && data.sessions.length > 0 && (
        <div className="cluster-grid">
          {data.sessions.map((session) => (
            <article key={session.sessionId} className="cluster-card">
              <h2>{session.reportSymptom || `${session.site} ${session.route}`}</h2>
              <p>
                <strong>Session:</strong> <code>{session.sessionId}</code>
              </p>
              <p>
                <strong>Route:</strong> {session.route}
              </p>
              <p>
                <strong>Markers:</strong> {session.markerCount} | <strong>Duration:</strong>{" "}
                {(session.durationMs / 1000).toFixed(1)}s
              </p>
              <p>
                <strong>Last Observed:</strong>{" "}
                {new Date(session.lastObservedAt).toLocaleString()}
              </p>
              <p>
                <strong>Report:</strong> {session.reportStatus} | <strong>Confidence:</strong>{" "}
                {(Math.max(0, Math.min(1, session.reportConfidence)) * 100).toFixed(0)}%
              </p>
              <Link to={`/sessions/${session.sessionId}`}>Open session replay</Link>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
