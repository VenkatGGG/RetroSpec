import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAppDispatch, useAppSelector } from "../app/hooks";
import { selectActiveMarkerId } from "../features/sessions/selectors";
import { setActiveMarker } from "../features/sessions/sessionSlice";
import { ReplayCanvas } from "../components/ReplayCanvas";
import { useGetSessionEventsQuery, useGetSessionQuery } from "../features/reporting/reportingApi";
import type { ErrorMarker } from "../features/sessions/types";

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

  const latestArtifact = activeSession.artifacts?.[0] ?? null;

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
      {latestArtifact && (
        <article className="artifact-card">
          <header className="artifact-card-header">
            <h2>Latest Replay Analysis</h2>
            <span>{new Date(latestArtifact.generatedAt).toLocaleString()}</span>
          </header>
          <p>
            <strong>Status:</strong> {latestArtifact.status} | <strong>Type:</strong>{" "}
            {latestArtifact.artifactType}
          </p>
          <p>
            <strong>Trigger:</strong> {latestArtifact.triggerKind}
          </p>
          <p>
            <strong>Artifact Key:</strong> <code>{latestArtifact.artifactKey}</code>
          </p>
          <div className="artifact-window-list">
            {latestArtifact.windows.length === 0 && (
              <p className="replay-status">No marker windows returned by replay analysis.</p>
            )}
            {latestArtifact.windows.map((window, index) => (
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
