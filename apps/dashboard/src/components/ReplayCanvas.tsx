import type { ErrorMarker } from "../features/sessions/types";

interface ReplayCanvasProps {
  markers: ErrorMarker[];
  activeMarkerId: string | null;
  onSeekToMarker: (marker: ErrorMarker) => void;
}

export function ReplayCanvas({
  markers,
  activeMarkerId,
  onSeekToMarker,
}: ReplayCanvasProps) {
  return (
    <section className="replay-panel">
      <header>
        <h3>Session Replay (rrweb-player mount point)</h3>
        <p>The full session is loaded once. Marker actions seek within the same timeline.</p>
      </header>

      <div className="replay-placeholder">
        <p>rrweb-player container</p>
      </div>

      <div className="marker-list">
        <h4>Detected Error Markers</h4>
        {markers.map((marker) => (
          <button
            key={marker.id}
            type="button"
            className={marker.id === activeMarkerId ? "marker active" : "marker"}
            onClick={() => onSeekToMarker(marker)}
          >
            <span>{marker.label}</span>
            <span>{(marker.replayOffsetMs / 1000).toFixed(1)}s</span>
          </button>
        ))}
      </div>
    </section>
  );
}
