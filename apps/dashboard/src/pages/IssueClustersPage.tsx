import { Link } from "react-router-dom";
import { useAppDispatch, useAppSelector } from "../app/hooks";
import { selectClusters } from "../features/sessions/selectors";
import { setActiveSession } from "../features/sessions/sessionSlice";

export function IssueClustersPage() {
  const clusters = useAppSelector(selectClusters);
  const dispatch = useAppDispatch();

  return (
    <section>
      <h1>Promoted Issue Clusters</h1>
      <p>
        Only repeated failures are shown here. A cluster appears when it passes confidence and
        recurrence thresholds.
      </p>

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
            <Link
              to={`/sessions/${cluster.representativeSessionId}`}
              onClick={() => dispatch(setActiveSession(cluster.representativeSessionId))}
            >
              Open representative replay
            </Link>
          </article>
        ))}
      </div>
    </section>
  );
}
