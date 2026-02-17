import { useEffect, useMemo, useRef, useState } from "react";
import "rrweb-player/dist/style.css";
import type { ErrorMarker } from "../features/sessions/types";

interface ReplayCanvasProps {
  markers: ErrorMarker[];
  activeMarkerId: string | null;
  events: unknown[];
  isEventsLoading: boolean;
  eventsError: string | null;
  seekRequest: { offsetMs: number; token: number } | null;
  onSeekToMarker: (marker: ErrorMarker) => void;
}

interface RRWebPlayerInstance {
  goto: (offset: number, play?: boolean) => void;
  play?: () => void;
  pause?: () => void;
}

interface RRWebPlayerConstructor {
  new (args: {
    target: HTMLElement;
    props: {
      autoPlay: boolean;
      events: unknown[];
      speed: number;
      showController: boolean;
      width: number;
      height: number;
    };
  }): RRWebPlayerInstance;
}

export function ReplayCanvas({
  markers,
  activeMarkerId,
  events,
  isEventsLoading,
  eventsError,
  seekRequest,
  onSeekToMarker,
}: ReplayCanvasProps) {
  const playerHostRef = useRef<HTMLDivElement | null>(null);
  const playerRef = useRef<RRWebPlayerInstance | null>(null);
  const [mountError, setMountError] = useState<string | null>(null);

  const activeMarker = useMemo(
    () => markers.find((marker) => marker.id === activeMarkerId) ?? null,
    [activeMarkerId, markers],
  );

  useEffect(() => {
    const host = playerHostRef.current;
    if (!host || events.length === 0) {
      return;
    }

    let cancelled = false;

    const mountPlayer = async () => {
      try {
        const module = await import("rrweb-player");
        if (cancelled) {
          return;
        }

        const RRWebPlayer = module.default as unknown as RRWebPlayerConstructor;
        host.innerHTML = "";

        playerRef.current = new RRWebPlayer({
          target: host,
          props: {
            autoPlay: false,
            events,
            speed: 1,
            showController: true,
            width: 960,
            height: 540,
          },
        });

        setMountError(null);
      } catch {
        setMountError("Unable to mount rrweb-player for this session.");
      }
    };

    void mountPlayer();

    return () => {
      cancelled = true;
      host.innerHTML = "";
      playerRef.current = null;
    };
  }, [events]);

  useEffect(() => {
    if (!activeMarker || !playerRef.current) {
      return;
    }

    const seekOffset = Math.max(0, activeMarker.replayOffsetMs - 2_000);
    playerRef.current.goto(seekOffset, false);
    playerRef.current.play?.();
  }, [activeMarker]);

  useEffect(() => {
    if (!seekRequest || !playerRef.current) {
      return;
    }

    playerRef.current.goto(Math.max(0, seekRequest.offsetMs), false);
    playerRef.current.play?.();
  }, [seekRequest]);

  return (
    <section className="replay-panel">
      <header>
        <h3>Session Replay</h3>
        <p>The full timeline is loaded once. Marker actions jump directly to the error moment.</p>
      </header>

      <div className="replay-player-host" ref={playerHostRef}>
        {isEventsLoading && <p className="replay-status">Loading replay events...</p>}
        {eventsError && <p className="replay-status error">{eventsError}</p>}
        {mountError && <p className="replay-status error">{mountError}</p>}
        {!isEventsLoading && !eventsError && events.length === 0 && (
          <p className="replay-status">No replay events available for this session.</p>
        )}
      </div>

      <div className="marker-list">
        <h4>Detected Error Markers</h4>
        {markers.length === 0 && <p className="replay-status">No markers captured for this session.</p>}
        {markers.map((marker) => (
          <button
            key={marker.id}
            type="button"
            className={marker.id === activeMarkerId ? "marker active" : "marker"}
            onClick={() => onSeekToMarker(marker)}
          >
            <span>{marker.evidence ? `${marker.label} (${marker.evidence})` : marker.label}</span>
            <span>{(marker.replayOffsetMs / 1000).toFixed(1)}s</span>
          </button>
        ))}
      </div>
    </section>
  );
}
