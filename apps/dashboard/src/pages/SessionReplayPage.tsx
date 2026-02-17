import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAppDispatch, useAppSelector } from "../app/hooks";
import { selectActiveMarkerId } from "../features/sessions/selectors";
import { setActiveMarker } from "../features/sessions/sessionSlice";
import { ReplayCanvas } from "../components/ReplayCanvas";
import {
  useGetSessionArtifactTokenQuery,
  useGetSessionEventsQuery,
  useGetSessionQuery,
} from "../features/reporting/reportingApi";
import type { ErrorMarker } from "../features/sessions/types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export function SessionReplayPage() {
  const { sessionId } = useParams();
  const dispatch = useAppDispatch();
  const activeMarkerId = useAppSelector(selectActiveMarkerId);
  const [seekRequest, setSeekRequest] = useState<{ offsetMs: number; token: number } | null>(null);
  const {
    data: activeSession,
    isLoading,
    isError,
  } = useGetSessionQuery(sessionId ?? "", { skip: !sessionId });
  const {
    data: replayEvents = [],
    isLoading: isEventsLoading,
    error: eventsError,
  } = useGetSessionEventsQuery(sessionId ?? "", { skip: !sessionId });
  const replayVideoArtifact =
    activeSession?.artifacts.find((artifact) => artifact.artifactType === "replay_video") ?? null;
  const {
    data: replayVideoToken,
    isFetching: isReplayTokenFetching,
    isError: isReplayTokenError,
  } = useGetSessionArtifactTokenQuery(
    { sessionId: activeSession?.id ?? "", artifactType: "replay_video" },
    {
      skip: !replayVideoArtifact || replayVideoArtifact.status !== "ready",
    },
  );

  if (!activeSession) {
    return (
      <section>
        {isLoading && <p>Loading session replay...</p>}
        {isError && <p>Session not found.</p>}
        <Link to="/">Return to issues</Link>
      </section>
    );
  }

  const requestSeek = (offsetMs: number) => {
    setSeekRequest({
      offsetMs: Math.max(0, offsetMs),
      token: Date.now(),
    });
  };

  const handleSeekToMarker = (marker: ErrorMarker) => {
    dispatch(setActiveMarker(marker.id));
    requestSeek(marker.replayOffsetMs - 2_000);
  };

  const analysisArtifact =
    activeSession.artifacts.find((artifact) => artifact.artifactType === "analysis_json") ??
    activeSession.artifacts?.[0] ??
    null;
  const reportCard = activeSession.reportCard ?? null;
  const replayVideoSource = replayVideoArtifact?.status === "ready" && replayVideoToken?.token
    ? `${apiBaseUrl}/v1/sessions/${activeSession.id}/artifacts/replay_video?artifactToken=${encodeURIComponent(replayVideoToken.token)}`
    : "";

  return (
    <section>
      <div className="session-heading">
        <div>
          <h1>Session {activeSession.id}</h1>
          <p>
            {activeSession.site} | {activeSession.route}
          </p>
        </div>
        <Link to="/">Back to issue clusters</Link>
      </div>
      {reportCard && (
        <article className="artifact-card report-card">
          <header className="artifact-card-header">
            <h2>Session Report Card</h2>
            <span>{new Date(reportCard.generatedAt).toLocaleString()}</span>
          </header>
          <p>
            <strong>Status:</strong> {reportCard.status} | <strong>Confidence:</strong>{" "}
            {(Math.max(0, Math.min(1, reportCard.confidence)) * 100).toFixed(0)}%
          </p>
          {reportCard.status === "pending" && (
            <p className="replay-status">Analysis in progress. This card updates asynchronously.</p>
          )}
          {reportCard.status === "failed" && (
            <p className="replay-status error">
              {reportCard.technicalRootCause || "Analysis worker failed before producing a report."}
            </p>
          )}
          {reportCard.status === "ready" && (
            <div className="report-grid">
              <p>
                <strong>Symptom:</strong> {reportCard.symptom}
              </p>
              <p>
                <strong>Technical Root Cause:</strong> {reportCard.technicalRootCause}
              </p>
              <p>
                <strong>Suggested Fix:</strong> {reportCard.suggestedFix}
              </p>
              <p>
                <strong>Text Path Evidence:</strong> {reportCard.textSummary}
              </p>
              <p>
                <strong>Visual Path Evidence:</strong> {reportCard.visualSummary}
              </p>
            </div>
          )}
        </article>
      )}
      {analysisArtifact && (
        <article className="artifact-card">
          <header className="artifact-card-header">
            <h2>Latest Replay Analysis</h2>
            <span>{new Date(analysisArtifact.generatedAt).toLocaleString()}</span>
          </header>
          <p>
            <strong>Status:</strong> {analysisArtifact.status} | <strong>Type:</strong>{" "}
            {analysisArtifact.artifactType}
          </p>
          <p>
            <strong>Trigger:</strong> {analysisArtifact.triggerKind}
          </p>
          <p>
            <strong>Artifact Key:</strong> <code>{analysisArtifact.artifactKey}</code>
          </p>
          <div className="artifact-window-list">
            {analysisArtifact.windows.length === 0 && (
              <p className="replay-status">No marker windows returned by replay analysis.</p>
            )}
            {analysisArtifact.windows.map((window, index) => (
              <button
                key={`${window.startMs}-${window.endMs}-${index}`}
                type="button"
                className="marker"
                onClick={() => {
                  dispatch(setActiveMarker(null));
                  requestSeek(window.startMs);
                }}
              >
                <span>Take me to error #{index + 1}</span>
                <span>
                  {(window.startMs / 1000).toFixed(1)}s to {(window.endMs / 1000).toFixed(1)}s
                </span>
              </button>
            ))}
          </div>
        </article>
      )}
      {replayVideoArtifact && (
        <article className="artifact-card">
          <header className="artifact-card-header">
            <h2>Rendered Replay Video</h2>
            <span>{new Date(replayVideoArtifact.generatedAt).toLocaleString()}</span>
          </header>
          <p>
            <strong>Status:</strong> {replayVideoArtifact.status} | <strong>Key:</strong>{" "}
            <code>{replayVideoArtifact.artifactKey}</code>
          </p>
          {replayVideoArtifact.status !== "ready" && (
            <p className="replay-status error">Video render failed for this session. Inspect analysis.json for details.</p>
          )}
          {replayVideoArtifact.status === "ready" && isReplayTokenFetching && (
            <p className="replay-status">Preparing secure playback...</p>
          )}
          {replayVideoArtifact.status === "ready" && isReplayTokenError && (
            <p className="replay-status error">Unable to create playback token for this artifact.</p>
          )}
          {replayVideoArtifact.status === "ready" && (
            <video
              className="replay-video"
              src={replayVideoSource}
              controls
              preload="metadata"
              playsInline
            />
          )}
        </article>
      )}

      <ReplayCanvas
        markers={activeSession.markers}
        activeMarkerId={activeMarkerId}
        events={replayEvents}
        isEventsLoading={isEventsLoading}
        eventsError={eventsError ? "Unable to load replay events." : null}
        seekRequest={seekRequest}
        onSeekToMarker={handleSeekToMarker}
      />
    </section>
  );
}
