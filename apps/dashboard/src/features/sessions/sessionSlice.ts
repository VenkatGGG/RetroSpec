import { createSlice, type PayloadAction } from "@reduxjs/toolkit";
import type { SessionsState } from "./types";

const initialState: SessionsState = {
  sessions: [
    {
      id: "sess_01",
      site: "demo-shop.io",
      route: "/checkout",
      startedAt: "2026-02-16T13:44:00Z",
      durationMs: 136000,
      markers: [
        {
          id: "mrk_01",
          clusterKey: "checkout-null-user",
          label: "Checkout clicked 5x, no progress",
          replayOffsetMs: 41200,
          kind: "ui_no_effect",
        },
        {
          id: "mrk_02",
          clusterKey: "checkout-null-user",
          label: "POST /api/orders -> 400",
          replayOffsetMs: 42800,
          kind: "api_error",
        },
      ],
    },
    {
      id: "sess_02",
      site: "demo-shop.io",
      route: "/checkout",
      startedAt: "2026-02-16T14:02:00Z",
      durationMs: 104000,
      markers: [
        {
          id: "mrk_03",
          clusterKey: "checkout-null-user",
          label: "Checkout clicked 4x, no progress",
          replayOffsetMs: 38500,
          kind: "ui_no_effect",
        },
      ],
    },
  ],
  issueClusters: [
    {
      key: "checkout-null-user",
      symptom: "User repeatedly clicks checkout and remains on page.",
      sessionCount: 2,
      userCount: 2,
      lastSeenAt: "2026-02-16T14:03:00Z",
      representativeSessionId: "sess_01",
    },
  ],
  activeSessionId: "sess_01",
  activeMarkerId: null,
};

const sessionsSlice = createSlice({
  name: "sessions",
  initialState,
  reducers: {
    setActiveSession(state, action: PayloadAction<string>) {
      state.activeSessionId = action.payload;
      state.activeMarkerId = null;
    },
    setActiveMarker(state, action: PayloadAction<string | null>) {
      state.activeMarkerId = action.payload;
    },
  },
});

export const { setActiveSession, setActiveMarker } = sessionsSlice.actions;
export const sessionsReducer = sessionsSlice.reducer;
