import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  useCleanupDataMutation,
  useGetIssuesQuery,
  useGetIssueStatsQuery,
  useMergeIssuesMutation,
  usePromoteIssuesMutation,
  useSubmitIssueFeedbackMutation,
  useUpdateIssueStateMutation,
} from "../features/reporting/reportingApi";
import type { IssueCluster } from "../features/sessions/types";

interface TriageDraft {
  state: "open" | "acknowledged" | "resolved" | "muted";
  assignee: string;
  note: string;
  mutedUntil: string;
}

function toDatetimeLocalValue(value?: string | null): string {
  if (!value) {
    return "";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  const local = new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function createDraft(cluster: IssueCluster): TriageDraft {
  const normalizedState =
    cluster.state === "open" ||
    cluster.state === "acknowledged" ||
    cluster.state === "resolved" ||
    cluster.state === "muted"
      ? cluster.state
      : "open";

  return {
    state: normalizedState,
    assignee: cluster.assignee ?? "",
    note: cluster.stateNote ?? "",
    mutedUntil: toDatetimeLocalValue(cluster.mutedUntil),
  };
}

export function IssueClustersPage() {
  const [lookbackHours, setLookbackHours] = useState(24);
  const [issueStateFilter, setIssueStateFilter] = useState<"" | "active" | "open" | "acknowledged" | "resolved" | "muted">("active");
  const {
    data: clusters = [],
    isLoading,
    isFetching,
    isError,
  } = useGetIssuesQuery(issueStateFilter);
  const { data: issueStats } = useGetIssueStatsQuery(lookbackHours);
  const [promoteIssues, { isLoading: isPromoting }] = usePromoteIssuesMutation();
  const [updateIssueState] = useUpdateIssueStateMutation();
  const [submitIssueFeedback] = useSubmitIssueFeedbackMutation();
  const [mergeIssues, { isLoading: isMerging }] = useMergeIssuesMutation();
  const [cleanupData, { isLoading: isCleaning }] = useCleanupDataMutation();
  const [maintenanceMessage, setMaintenanceMessage] = useState<string>("");
  const [triageDrafts, setTriageDrafts] = useState<Record<string, TriageDraft>>({});
  const [savingIssueKey, setSavingIssueKey] = useState<string | null>(null);
  const [triageMessage, setTriageMessage] = useState<string>("");
  const [mergeTargetClusterKey, setMergeTargetClusterKey] = useState<string>("");
  const [mergeSourceKeysInput, setMergeSourceKeysInput] = useState<string>("");
  const [mergeNote, setMergeNote] = useState<string>("Merged due to operator feedback.");

  useEffect(() => {
    if (clusters.length === 0) {
      return;
    }
    setTriageDrafts((previous) => {
      const next = { ...previous };
      for (const cluster of clusters) {
        if (!next[cluster.key]) {
          next[cluster.key] = createDraft(cluster);
        }
      }
      return next;
    });
  }, [clusters]);

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

  const handleDraftChange = (clusterKey: string, patch: Partial<TriageDraft>) => {
    setTriageDrafts((previous) => ({
      ...previous,
      [clusterKey]: {
        ...(previous[clusterKey] ?? {
          state: "open",
          assignee: "",
          note: "",
          mutedUntil: "",
        }),
        ...patch,
      },
    }));
  };

  const handleSaveTriage = async (clusterKey: string) => {
    const draft = triageDrafts[clusterKey];
    if (!draft) {
      return;
    }

    setSavingIssueKey(clusterKey);
    try {
      const mutedUntilISO =
        draft.state === "muted" && draft.mutedUntil
          ? new Date(draft.mutedUntil).toISOString()
          : undefined;

      await updateIssueState({
        clusterKey,
        state: draft.state,
        assignee: draft.assignee.trim(),
        note: draft.note.trim(),
        mutedUntil: mutedUntilISO,
      }).unwrap();
      setTriageMessage(`Saved triage updates for ${clusterKey}.`);
    } catch {
      setTriageMessage(`Failed to save triage updates for ${clusterKey}.`);
    } finally {
      setSavingIssueKey(null);
    }
  };

  const handleFeedback = async (
    clusterKey: string,
    kind:
      | "false_positive"
      | "true_positive"
      | "invalid"
      | "suppressed"
      | "unsuppressed",
    note: string,
    metadata?: Record<string, unknown>,
  ) => {
    setSavingIssueKey(clusterKey);
    try {
      await submitIssueFeedback({
        clusterKey,
        kind,
        note,
        createdBy: "dashboard",
        metadata,
      }).unwrap();
      setTriageMessage(`Recorded ${kind} feedback for ${clusterKey}.`);
    } catch {
      setTriageMessage(`Failed to record ${kind} feedback for ${clusterKey}.`);
    } finally {
      setSavingIssueKey(null);
    }
  };

  const handleSuppress = async (cluster: IssueCluster, days: number) => {
    setSavingIssueKey(cluster.key);
    const mutedUntil = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
    const note = `Suppressed for ${days} day(s) via feedback loop.`;
    try {
      await updateIssueState({
        clusterKey: cluster.key,
        state: "muted",
        assignee: cluster.assignee || "",
        mutedUntil,
        note,
      }).unwrap();
      await submitIssueFeedback({
        clusterKey: cluster.key,
        kind: "suppressed",
        note,
        createdBy: "dashboard",
        metadata: { days },
      }).unwrap();
      setTriageMessage(`Suppressed ${cluster.key} for ${days} day(s).`);
      setTriageDrafts((previous) => ({
        ...previous,
        [cluster.key]: {
          ...(previous[cluster.key] ?? createDraft(cluster)),
          state: "muted",
          mutedUntil: toDatetimeLocalValue(mutedUntil),
          note,
        },
      }));
    } catch {
      setTriageMessage(`Failed to suppress ${cluster.key}.`);
    } finally {
      setSavingIssueKey(null);
    }
  };

  const handleUnsuppress = async (cluster: IssueCluster) => {
    setSavingIssueKey(cluster.key);
    const note = "Unsuppressed via feedback loop.";
    try {
      await updateIssueState({
        clusterKey: cluster.key,
        state: "open",
        assignee: cluster.assignee || "",
        note,
      }).unwrap();
      await submitIssueFeedback({
        clusterKey: cluster.key,
        kind: "unsuppressed",
        note,
        createdBy: "dashboard",
      }).unwrap();
      setTriageMessage(`Unsuppressed ${cluster.key}.`);
      setTriageDrafts((previous) => ({
        ...previous,
        [cluster.key]: {
          ...(previous[cluster.key] ?? createDraft(cluster)),
          state: "open",
          mutedUntil: "",
          note,
        },
      }));
    } catch {
      setTriageMessage(`Failed to unsuppress ${cluster.key}.`);
    } finally {
      setSavingIssueKey(null);
    }
  };

  const handleMerge = async () => {
    const targetClusterKey = mergeTargetClusterKey.trim();
    const sourceClusterKeys = mergeSourceKeysInput
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0 && value !== targetClusterKey);

    if (!targetClusterKey || sourceClusterKeys.length === 0) {
      setTriageMessage("Merge requires one target cluster key and at least one source key.");
      return;
    }

    try {
      const result = await mergeIssues({
        targetClusterKey,
        sourceClusterKeys,
        note: mergeNote.trim(),
        createdBy: "dashboard",
      }).unwrap();
      setTriageMessage(
        `Merged ${result.result.sourceClusterKeys.length} source cluster(s) into ${result.result.targetClusterKey}; moved ${result.result.movedMarkerCount} markers.`,
      );
      setMergeSourceKeysInput("");
    } catch {
      setTriageMessage("Issue cluster merge failed. Verify source and target keys.");
    }
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
        <label htmlFor="issue-state-filter-select">Issue State</label>
        <select
          id="issue-state-filter-select"
          value={issueStateFilter}
          onChange={(event) =>
            setIssueStateFilter(
              event.target.value as "" | "active" | "open" | "acknowledged" | "resolved" | "muted",
            )
          }
        >
          <option value="active">active</option>
          <option value="">all</option>
          <option value="open">open</option>
          <option value="acknowledged">acknowledged</option>
          <option value="resolved">resolved</option>
          <option value="muted">muted</option>
        </select>
        <button type="button" onClick={handlePromote} disabled={isPromoting}>
          {isPromoting ? "Promoting..." : "Recompute Clusters"}
        </button>
        <button type="button" onClick={handleCleanup} disabled={isCleaning}>
          {isCleaning ? "Cleaning..." : "Run Retention Cleanup"}
        </button>
        {isFetching && <span>Refreshing...</span>}
      </div>
      <div className="admin-card merge-card">
        <h2>Merge Clusters</h2>
        <label htmlFor="merge-target-cluster">Target Cluster Key</label>
        <input
          id="merge-target-cluster"
          value={mergeTargetClusterKey}
          onChange={(event) => setMergeTargetClusterKey(event.target.value)}
          placeholder="api_error:1234abcd..."
        />
        <label htmlFor="merge-source-clusters">Source Cluster Keys (comma-separated)</label>
        <input
          id="merge-source-clusters"
          value={mergeSourceKeysInput}
          onChange={(event) => setMergeSourceKeysInput(event.target.value)}
          placeholder="api_error:old1..., api_error:old2..."
        />
        <label htmlFor="merge-note">Merge Note</label>
        <input
          id="merge-note"
          value={mergeNote}
          onChange={(event) => setMergeNote(event.target.value)}
          placeholder="Merged due to duplicate fingerprinting"
        />
        <button type="button" onClick={handleMerge} disabled={isMerging}>
          {isMerging ? "Merging..." : "Merge clusters"}
        </button>
      </div>
      {maintenanceMessage && <p>{maintenanceMessage}</p>}
      {triageMessage && <p>{triageMessage}</p>}
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
            <p>
              <strong>Triage:</strong> {cluster.state} | <strong>Assignee:</strong>{" "}
              {cluster.assignee || "unassigned"}
            </p>
            <div className="feedback-controls">
              <button
                type="button"
                onClick={() =>
                  void handleFeedback(
                    cluster.key,
                    "false_positive",
                    "Marked false positive by dashboard operator.",
                  )
                }
                disabled={savingIssueKey === cluster.key}
              >
                Mark False Positive
              </button>
              <button
                type="button"
                onClick={() =>
                  void handleFeedback(
                    cluster.key,
                    "true_positive",
                    "Confirmed true positive by dashboard operator.",
                  )
                }
                disabled={savingIssueKey === cluster.key}
              >
                Mark True Positive
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleSuppress(cluster, 7);
                }}
                disabled={savingIssueKey === cluster.key}
              >
                Suppress 7d
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleSuppress(cluster, 30);
                }}
                disabled={savingIssueKey === cluster.key}
              >
                Suppress 30d
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleUnsuppress(cluster);
                }}
                disabled={savingIssueKey === cluster.key}
              >
                Unsuppress
              </button>
            </div>
            <div className="triage-controls">
              <label htmlFor={`triage-state-${cluster.key}`}>State</label>
              <select
                id={`triage-state-${cluster.key}`}
                value={triageDrafts[cluster.key]?.state ?? createDraft(cluster).state}
                onChange={(event) =>
                  handleDraftChange(cluster.key, {
                    state: event.target.value as TriageDraft["state"],
                  })
                }
              >
                <option value="open">open</option>
                <option value="acknowledged">acknowledged</option>
                <option value="resolved">resolved</option>
                <option value="muted">muted</option>
              </select>
              <label htmlFor={`triage-assignee-${cluster.key}`}>Assignee</label>
              <input
                id={`triage-assignee-${cluster.key}`}
                value={triageDrafts[cluster.key]?.assignee ?? createDraft(cluster).assignee}
                onChange={(event) =>
                  handleDraftChange(cluster.key, {
                    assignee: event.target.value,
                  })
                }
                placeholder="oncall-web"
              />
              {((triageDrafts[cluster.key]?.state ?? createDraft(cluster).state) === "muted") && (
                <>
                  <label htmlFor={`triage-muted-until-${cluster.key}`}>Muted Until</label>
                  <input
                    id={`triage-muted-until-${cluster.key}`}
                    type="datetime-local"
                    value={
                      triageDrafts[cluster.key]?.mutedUntil ?? createDraft(cluster).mutedUntil
                    }
                    onChange={(event) =>
                      handleDraftChange(cluster.key, {
                        mutedUntil: event.target.value,
                      })
                    }
                  />
                </>
              )}
              <label htmlFor={`triage-note-${cluster.key}`}>Note</label>
              <input
                id={`triage-note-${cluster.key}`}
                value={triageDrafts[cluster.key]?.note ?? createDraft(cluster).note}
                onChange={(event) =>
                  handleDraftChange(cluster.key, {
                    note: event.target.value,
                  })
                }
                placeholder="Investigating checkout flow"
              />
              <button
                type="button"
                onClick={() => {
                  void handleSaveTriage(cluster.key);
                }}
                disabled={
                  savingIssueKey === cluster.key ||
                  ((triageDrafts[cluster.key]?.state ?? createDraft(cluster).state) === "muted" &&
                    !(triageDrafts[cluster.key]?.mutedUntil ?? createDraft(cluster).mutedUntil))
                }
              >
                {savingIssueKey === cluster.key ? "Saving..." : "Save triage"}
              </button>
            </div>
            <p>
              <Link to={`/issues/${encodeURIComponent(cluster.key)}/sessions`}>
                View cluster sessions
              </Link>
            </p>
            <Link to={`/sessions/${cluster.representativeSessionId}`}>Open representative replay</Link>
          </article>
        ))}
      </div>
    </section>
  );
}
