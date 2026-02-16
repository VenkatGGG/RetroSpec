import type { RootState } from "../../app/store";

export const selectClusters = (state: RootState) => state.sessions.issueClusters;
export const selectSessions = (state: RootState) => state.sessions.sessions;
export const selectActiveSession = (state: RootState) => {
  const activeId = state.sessions.activeSessionId;
  return state.sessions.sessions.find((session) => session.id === activeId) ?? null;
};
export const selectActiveMarkerId = (state: RootState) => state.sessions.activeMarkerId;
