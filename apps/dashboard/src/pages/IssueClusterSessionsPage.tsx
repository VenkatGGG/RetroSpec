import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  useGetIssueSessionsQuery,
  useListIssueFeedbackQuery,
  useSplitIssueMutation,
} from "../features/reporting/reportingApi";

export function IssueClusterSessionsPage() {
  const { clusterKey } = useParams();
  const decodedClusterKey = clusterKey ? decodeURIComponent(clusterKey) : "";
  const [reportStatus, setReportStatus] = useState<
    "" | "pending" | "ready" | "failed" | "discarded"
  >("");
  const [minConfidencePercent, setMinConfidencePercent] = useState(0);
  const [newClusterKey, setNewClusterKey] = useState("");
  const [splitNote, setSplitNote] = useState("Split cluster after reviewing session context.");
  const [selectedSessionIds, setSelectedSessionIds] = useState<string[]>([]);
  const [splitMessage, setSplitMessage] = useState("");
  const [splitIssue, { isLoading: isSplitting }] = useSplitIssueMutation();
  const minConfidence = useMemo(
    () => Math.max(0, Math.min(100, minConfidencePercent)) / 100,
    [minConfidencePercent],
  );
  const {
    data,
    isLoading,
    isError,
  } = useGetIssueSessionsQuery(
    {
      clusterKey: decodedClusterKey,
      limit: 50,
      reportStatus,
      minConfidence,
    },
    {
      skip: !decodedClusterKey,
      pollingInterval: decodedClusterKey ? 15_000 : 0,
      refetchOnMountOrArgChange: true,
    },
  );
  const { data: feedbackData } = useListIssueFeedbackQuery(
    { clusterKey: decodedClusterKey, limit: 12 },
    {
      skip: !decodedClusterKey,
      pollingInterval: decodedClusterKey ? 20_000 : 0,
    },
  );

  const toggleSession = (sessionId: string) => {
    setSelectedSessionIds((previous) =>
      previous.includes(sessionId)
        ? previous.filter((candidate) => candidate !== sessionId)
        : [...previous, sessionId],
    );
  };

  const handleSplit = async () => {
    if (!decodedClusterKey) {
      return;
    }
    if (selectedSessionIds.length === 0) {
      setSplitMessage("Select at least one session to split.");
      return;
    }

    try {
      const result = await splitIssue({
        clusterKey: decodedClusterKey,
        newClusterKey: newClusterKey.trim() || undefined,
        sessionIds: selectedSessionIds,
        note: splitNote.trim(),
        createdBy: "dashboard",
      }).unwrap();
      setSplitMessage(
        `Split moved ${result.result.movedMarkerCount} marker(s) from ${result.result.sourceClusterKey} to ${result.result.newClusterKey}.`,
      );
      setSelectedSessionIds([]);
      setNewClusterKey(result.result.newClusterKey);
    } catch {
      setSplitMessage("Split operation failed. Verify session selection and cluster key.");
    }
  };

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
      <div className="issue-actions">
        <label htmlFor="report-status-select">Report Status</label>
        <select
          id="report-status-select"
          value={reportStatus}
          onChange={(event) =>
            setReportStatus(
              event.target.value as "" | "pending" | "ready" | "failed" | "discarded",
            )
          }
        >
          <option value="">All</option>
          <option value="ready">ready</option>
          <option value="pending">pending</option>
          <option value="failed">failed</option>
          <option value="discarded">discarded</option>
        </select>
        <label htmlFor="min-confidence-input">Min Confidence %</label>
        <input
          id="min-confidence-input"
          type="number"
          min={0}
          max={100}
          value={minConfidencePercent}
          onChange={(event) => setMinConfidencePercent(Number(event.target.value))}
        />
      </div>
      <div className="admin-card split-card">
        <h2>Split Cluster</h2>
        <p>
          Move selected sessions into a new cluster key when the current grouping appears incorrect.
        </p>
        <label htmlFor="split-new-cluster-key">New Cluster Key (optional)</label>
        <input
          id="split-new-cluster-key"
          value={newClusterKey}
          onChange={(event) => setNewClusterKey(event.target.value)}
          placeholder="api_error:split-variant"
        />
        <label htmlFor="split-note">Split Note</label>
        <input
          id="split-note"
          value={splitNote}
          onChange={(event) => setSplitNote(event.target.value)}
          placeholder="Split reason"
        />
        <p>
          Selected sessions: <strong>{selectedSessionIds.length}</strong>
        </p>
        <button type="button" onClick={handleSplit} disabled={isSplitting}>
          {isSplitting ? "Splitting..." : "Split selected sessions"}
        </button>
        {splitMessage && <p>{splitMessage}</p>}
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
              <p>
                <label>
                  <input
                    type="checkbox"
                    checked={selectedSessionIds.includes(session.sessionId)}
                    onChange={() => toggleSession(session.sessionId)}
                  />{" "}
                  Select for split
                </label>
              </p>
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
      {feedbackData && feedbackData.events.length > 0 && (
        <div className="admin-card">
          <h2>Feedback History</h2>
          {feedbackData.events.map((event) => (
            <p key={event.id}>
              <strong>{event.feedbackKind}</strong> | {new Date(event.createdAt).toLocaleString()} |{" "}
              {event.note || "no note"}
            </p>
          ))}
        </div>
      )}
    </section>
  );
}
