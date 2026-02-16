import type { RootState } from "../../app/store";

export const selectActiveMarkerId = (state: RootState) => state.sessions.activeMarkerId;
