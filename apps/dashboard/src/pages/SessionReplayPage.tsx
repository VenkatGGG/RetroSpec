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

  const handleSeekToMarker = (marker: ErrorMarker) => {
    // Placeholder seek action. In rrweb-player this maps to player.goto(marker.replayOffsetMs).
    dispatch(setActiveMarker(marker.id));
  };

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

      <ReplayCanvas
        markers={activeSession.markers}
        activeMarkerId={activeMarkerId}
        events={replayEvents}
        isEventsLoading={isEventsLoading}
        eventsError={eventsError ? "Unable to load replay events." : null}
        onSeekToMarker={handleSeekToMarker}
      />
    </section>
  );
}
