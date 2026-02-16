import { useEffect } from "react";
import { Link, useParams } from "react-router-dom";
import { useAppDispatch, useAppSelector } from "../app/hooks";
import {
  selectActiveMarkerId,
  selectActiveSession,
  selectSessions,
} from "../features/sessions/selectors";
import { setActiveMarker, setActiveSession } from "../features/sessions/sessionSlice";
import { ReplayCanvas } from "../components/ReplayCanvas";
import type { ErrorMarker } from "../features/sessions/types";

export function SessionReplayPage() {
  const { sessionId } = useParams();
  const dispatch = useAppDispatch();
  const sessions = useAppSelector(selectSessions);
  const activeSession = useAppSelector(selectActiveSession);
  const activeMarkerId = useAppSelector(selectActiveMarkerId);

  useEffect(() => {
    if (!sessionId || activeSession?.id === sessionId) {
      return;
    }

    const found = sessions.find((session) => session.id === sessionId);
    if (found) {
      dispatch(setActiveSession(found.id));
    }
  }, [activeSession?.id, dispatch, sessionId, sessions]);

  if (!activeSession) {
    return (
      <section>
        <p>Session not found.</p>
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
        onSeekToMarker={handleSeekToMarker}
      />
    </section>
  );
}
