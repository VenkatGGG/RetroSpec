import { createSlice, type PayloadAction } from "@reduxjs/toolkit";
import type { SessionsState } from "./types";

const initialState: SessionsState = {
  activeMarkerId: null,
};

const sessionsSlice = createSlice({
  name: "sessions",
  initialState,
  reducers: {
    setActiveMarker(state, action: PayloadAction<string | null>) {
      state.activeMarkerId = action.payload;
    },
  },
});

export const { setActiveMarker } = sessionsSlice.actions;
export const sessionsReducer = sessionsSlice.reducer;
