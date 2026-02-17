import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  useCreateProjectKeyMutation,
  useCreateProjectMutation,
  useGetQueueDeadLettersQuery,
  useGetQueueHealthQuery,
  useListProjectKeysQuery,
  useListProjectsQuery,
  useRedriveQueueDeadLettersMutation,
} from "../features/reporting/reportingApi";

export function AdminPage() {
  const [name, setName] = useState("");
  const [site, setSite] = useState("");
  const [label, setLabel] = useState("default-key");
  const [projectId, setProjectId] = useState("");
  const [newKeyLabel, setNewKeyLabel] = useState("rotation-key");
  const [deadLetterQueue, setDeadLetterQueue] = useState<"replay" | "analysis">("replay");
  const [deadLetterLimit, setDeadLetterLimit] = useState("20");
  const [redriveLimit, setRedriveLimit] = useState("25");
  const [resultMessage, setResultMessage] = useState("");

  const { data: projects = [], isLoading: isProjectsLoading } = useListProjectsQuery();
  const { data: projectKeys = [] } = useListProjectKeysQuery(projectId, { skip: !projectId });
  const {
    data: queueHealth,
    isLoading: isQueueHealthLoading,
    isFetching: isQueueHealthFetching,
    isError: isQueueHealthError,
    refetch: refetchQueueHealth,
  } = useGetQueueHealthQuery(undefined, { pollingInterval: 15000 });
  const parsedDeadLetterLimit = Number.parseInt(deadLetterLimit, 10);
  const normalizedDeadLetterLimit =
    Number.isFinite(parsedDeadLetterLimit) && parsedDeadLetterLimit > 0
      ? Math.min(200, parsedDeadLetterLimit)
      : 20;
  const {
    data: deadLetters,
    isLoading: isDeadLettersLoading,
    isFetching: isDeadLettersFetching,
    isError: isDeadLettersError,
    refetch: refetchDeadLetters,
  } = useGetQueueDeadLettersQuery(
    {
      queue: deadLetterQueue,
      limit: normalizedDeadLetterLimit,
    },
    {
      pollingInterval: 15000,
    },
  );
  const [createProject, { isLoading: isCreatingProject }] = useCreateProjectMutation();
  const [createProjectKey, { isLoading: isCreatingKey }] = useCreateProjectKeyMutation();
  const [redriveQueueDeadLetters, { isLoading: isRedrivingQueue }] =
    useRedriveQueueDeadLettersMutation();

  const handleCreateProject = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const result = await createProject({ name, site, label }).unwrap();
      setProjectId(result.project.id);
      setResultMessage(
        `Created ${result.project.id}. Store this API key now: ${result.apiKey}`,
      );
    } catch {
      setResultMessage("Project creation failed. Check ADMIN_API_KEY and payload values.");
    }
  };

  const handleCreateProjectKey = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const result = await createProjectKey({ projectId, label: newKeyLabel }).unwrap();
      setResultMessage(
        `Created key ${result.apiKeyId} for ${result.projectId}. Store this API key now: ${result.apiKey}`,
      );
    } catch {
      setResultMessage("API key creation failed. Verify project ID and admin auth.");
    }
  };

  const handleQueueRedrive = async (queue: "replay" | "analysis") => {
    const parsedLimit = Number.parseInt(redriveLimit, 10);
    const limit =
      Number.isFinite(parsedLimit) && parsedLimit > 0 ? Math.min(500, parsedLimit) : 25;

    try {
      const response = await redriveQueueDeadLetters({ queue, limit }).unwrap();
      setResultMessage(
        `Re-drive ${response.result.queueKind}: moved ${response.result.redriven}, skipped ${response.result.skipped}, remaining dead-letter ${response.result.remainingFailed}.`,
      );
      void refetchQueueHealth();
      void refetchDeadLetters();
    } catch {
      setResultMessage("Queue re-drive failed. Verify admin auth and Redis connectivity.");
    }
  };

  return (
    <section>
      <div className="session-heading">
        <h1>Admin</h1>
        <Link to="/">Back to issues</Link>
      </div>
      <p>Create projects and issue project-specific API keys for SDK integrations.</p>
      <div className="admin-card queue-health-card">
        <div className="artifact-card-header">
          <h2>Queue Health</h2>
          <button
            type="button"
            onClick={() => void refetchQueueHealth()}
            disabled={isQueueHealthFetching}
          >
            {isQueueHealthFetching ? "Refreshing..." : "Refresh"}
          </button>
        </div>
        {isQueueHealthLoading && <p>Loading queue health...</p>}
        {isQueueHealthError && <p>Queue health unavailable. Verify admin auth and Redis connection.</p>}
        {queueHealth && (
          <>
            <p>
              <strong>Status:</strong> <span className={`queue-status-${queueHealth.status}`}>{queueHealth.status}</span>
            </p>
            <p>
              <strong>Generated:</strong> {new Date(queueHealth.generatedAt).toLocaleString()}
            </p>
            <p>
              Warning when pending ≥ {queueHealth.thresholds.warningPending} or retry ≥{" "}
              {queueHealth.thresholds.warningRetry}. Critical when pending ≥{" "}
              {queueHealth.thresholds.criticalPending}, retry ≥{" "}
              {queueHealth.thresholds.criticalRetry}, or dead-letter ≥{" "}
              {queueHealth.thresholds.criticalFailed}.
            </p>
            <div className="queue-redrive-controls">
              <label htmlFor="dead-letter-redrive-limit">Dead-letter re-drive limit</label>
              <input
                id="dead-letter-redrive-limit"
                type="number"
                min={1}
                max={500}
                value={redriveLimit}
                onChange={(event) => setRedriveLimit(event.target.value)}
              />
              <button
                type="button"
                onClick={() => void handleQueueRedrive("replay")}
                disabled={isRedrivingQueue}
              >
                {isRedrivingQueue ? "Re-driving..." : "Re-drive replay"}
              </button>
              <button
                type="button"
                onClick={() => void handleQueueRedrive("analysis")}
                disabled={isRedrivingQueue}
              >
                {isRedrivingQueue ? "Re-driving..." : "Re-drive analysis"}
              </button>
            </div>
            <div className="queue-health-grid">
              <div className="stats-card">
                <h3>Replay Queue</h3>
                <p>Stream: {queueHealth.replay.streamDepth}</p>
                <p>Pending: {queueHealth.replay.pending}</p>
                <p>Retry: {queueHealth.replay.retryDepth}</p>
                <p>Dead-letter: {queueHealth.replay.failedDepth}</p>
              </div>
              <div className="stats-card">
                <h3>Analysis Queue</h3>
                <p>Stream: {queueHealth.analysis.streamDepth}</p>
                <p>Pending: {queueHealth.analysis.pending}</p>
                <p>Retry: {queueHealth.analysis.retryDepth}</p>
                <p>Dead-letter: {queueHealth.analysis.failedDepth}</p>
              </div>
            </div>
            <div className="dead-letter-controls">
              <label htmlFor="dead-letter-queue-kind">Dead-letter queue</label>
              <select
                id="dead-letter-queue-kind"
                value={deadLetterQueue}
                onChange={(event) =>
                  setDeadLetterQueue(event.target.value === "analysis" ? "analysis" : "replay")
                }
              >
                <option value="replay">Replay</option>
                <option value="analysis">Analysis</option>
              </select>
              <label htmlFor="dead-letter-limit">Rows</label>
              <input
                id="dead-letter-limit"
                type="number"
                min={1}
                max={200}
                value={deadLetterLimit}
                onChange={(event) => setDeadLetterLimit(event.target.value)}
              />
              <button
                type="button"
                onClick={() => void refetchDeadLetters()}
                disabled={isDeadLettersFetching}
              >
                {isDeadLettersFetching ? "Refreshing..." : "Refresh dead-letter"}
              </button>
            </div>
            {isDeadLettersLoading && <p>Loading dead-letter entries...</p>}
            {isDeadLettersError && <p>Dead-letter entries unavailable. Verify admin auth and Redis.</p>}
            {deadLetters && (
              <div className="dead-letter-list">
                <p>
                  Showing {deadLetters.entries.length} of {deadLetters.total} dead-letter entries
                  for <strong>{deadLetters.queueKind}</strong> queue. Unparsable backlog:{" "}
                  {deadLetters.unparsable}.
                </p>
                {deadLetters.entries.length === 0 && (
                  <p className="replay-status">No dead-letter entries for this queue.</p>
                )}
                {deadLetters.entries.map((entry, index) => (
                  <article
                    key={`${entry.sessionId || "unknown"}:${entry.failedAt || "na"}:${index}`}
                    className="dead-letter-entry"
                  >
                    <p>
                      <strong>Session:</strong> {entry.sessionId || "unknown"} |{" "}
                      <strong>Project:</strong> {entry.projectId || "unknown"} |{" "}
                      <strong>Attempt:</strong> {entry.attempt || 0}
                    </p>
                    <p>
                      <strong>Trigger:</strong> {entry.triggerKind || "unknown"} |{" "}
                      <strong>Failed At:</strong>{" "}
                      {entry.failedAt ? new Date(entry.failedAt).toLocaleString() : "unknown"}
                    </p>
                    <p>
                      <strong>Error:</strong> {entry.error || "unknown"}
                    </p>
                  </article>
                ))}
              </div>
            )}
          </>
        )}
      </div>
      {isProjectsLoading && <p>Loading projects...</p>}

      <div className="admin-grid">
        <form className="admin-card" onSubmit={handleCreateProject}>
          <h2>Create Project</h2>
          <label htmlFor="project-name">Project name</label>
          <input
            id="project-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Checkout Monitoring"
            required
          />

          <label htmlFor="project-site">Site</label>
          <input
            id="project-site"
            value={site}
            onChange={(event) => setSite(event.target.value)}
            placeholder="shop.example.com"
            required
          />

          <label htmlFor="project-label">Initial key label</label>
          <input
            id="project-label"
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            placeholder="default-key"
            required
          />

          <button type="submit" disabled={isCreatingProject}>
            {isCreatingProject ? "Creating..." : "Create project + key"}
          </button>
        </form>

        <form className="admin-card" onSubmit={handleCreateProjectKey}>
          <h2>Create Project Key</h2>
          <label htmlFor="project-id">Project</label>
          <select
            id="project-id"
            value={projectId}
            onChange={(event) => setProjectId(event.target.value)}
            required
          >
            <option value="">Select a project</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name} ({project.id})
              </option>
            ))}
          </select>

          <label htmlFor="new-key-label">Key label</label>
          <input
            id="new-key-label"
            value={newKeyLabel}
            onChange={(event) => setNewKeyLabel(event.target.value)}
            placeholder="rotation-key"
            required
          />

          <button type="submit" disabled={isCreatingKey}>
            {isCreatingKey ? "Creating..." : "Issue key"}
          </button>
        </form>
      </div>

      {projectId && (
        <div className="admin-card">
          <h2>Existing Keys</h2>
          {projectKeys.length === 0 && <p>No keys found for this project.</p>}
          {projectKeys.map((key) => (
            <p key={key.id}>
              <strong>{key.label}</strong> | {key.status} | Last used:{" "}
              {key.lastUsedAt ? new Date(key.lastUsedAt).toLocaleString() : "never"}
            </p>
          ))}
        </div>
      )}

      {resultMessage && <p className="admin-result">{resultMessage}</p>}
    </section>
  );
}
