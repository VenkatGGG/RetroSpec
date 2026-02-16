import { configureStore } from "@reduxjs/toolkit";
import { sessionsReducer } from "../features/sessions/sessionSlice";
import { reportingApi } from "../features/reporting/reportingApi";

export const store = configureStore({
  reducer: {
    sessions: sessionsReducer,
    [reportingApi.reducerPath]: reportingApi.reducer,
  },
  middleware: (getDefaultMiddleware) => getDefaultMiddleware().concat(reportingApi.middleware),
});

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;
