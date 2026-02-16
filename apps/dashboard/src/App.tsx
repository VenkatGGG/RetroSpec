import { Navigate, Route, Routes } from "react-router-dom";
import { IssueClustersPage } from "./pages/IssueClustersPage";
import { SessionReplayPage } from "./pages/SessionReplayPage";
import "./App.css";

export default function App() {
  return (
    <main className="app-shell">
      <Routes>
        <Route path="/" element={<IssueClustersPage />} />
        <Route path="/sessions/:sessionId" element={<SessionReplayPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </main>
  );
}
