import { createApi, fetchBaseQuery } from "@reduxjs/toolkit/query/react";
import type { IssueCluster, SessionSummary } from "../sessions/types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";
const ingestApiKey = import.meta.env.VITE_INGEST_API_KEY;

export const reportingApi = createApi({
  reducerPath: "reportingApi",
  baseQuery: fetchBaseQuery({
    baseUrl: apiBaseUrl,
    prepareHeaders: (headers) => {
      if (ingestApiKey) {
        headers.set("X-Retrospec-Key", ingestApiKey);
      }
      return headers;
    },
  }),
  tagTypes: ["Issue", "Session"],
  endpoints: (builder) => ({
    getIssues: builder.query<IssueCluster[], void>({
      query: () => "/v1/issues",
      transformResponse: (response: { issues: IssueCluster[] }) => response.issues,
      providesTags: (result) =>
        result
          ? [
              ...result.map((issue) => ({ type: "Issue" as const, id: issue.key })),
              { type: "Issue" as const, id: "LIST" },
            ]
          : [{ type: "Issue" as const, id: "LIST" }],
    }),
    getSession: builder.query<SessionSummary, string>({
      query: (sessionId) => `/v1/sessions/${sessionId}`,
      transformResponse: (response: { session: SessionSummary }) => response.session,
      providesTags: (_result, _error, sessionId) => [{ type: "Session", id: sessionId }],
    }),
    getSessionEvents: builder.query<unknown[], string>({
      query: (sessionId) => `/v1/sessions/${sessionId}/events`,
      transformResponse: (response: { events: unknown }) =>
        Array.isArray(response.events) ? response.events : [],
      providesTags: (_result, _error, sessionId) => [{ type: "Session", id: `${sessionId}:events` }],
    }),
    promoteIssues: builder.mutation<{ promoted: IssueCluster[] }, void>({
      query: () => ({
        url: "/v1/issues/promote",
        method: "POST",
      }),
      invalidatesTags: [{ type: "Issue", id: "LIST" }],
    }),
    cleanupData: builder.mutation<
      {
        deletedSessions: number;
        deletedIssueClusters: number;
        deletedEventObjects: number;
        failedEventObjectDelete: number;
        retentionDays: number;
      },
      void
    >({
      query: () => ({
        url: "/v1/maintenance/cleanup",
        method: "POST",
      }),
      invalidatesTags: [{ type: "Issue", id: "LIST" }],
    }),
  }),
});

export const {
  useGetIssuesQuery,
  useGetSessionQuery,
  useGetSessionEventsQuery,
  usePromoteIssuesMutation,
  useCleanupDataMutation,
} = reportingApi;
