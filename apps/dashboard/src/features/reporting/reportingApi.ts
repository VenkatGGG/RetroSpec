import { createApi, fetchBaseQuery } from "@reduxjs/toolkit/query/react";
import type {
  IssueCluster,
  IssueClusterSession,
  IssueKindStat,
  SessionSummary,
} from "../sessions/types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";
const ingestApiKey = import.meta.env.VITE_INGEST_API_KEY;
const adminApiKey = import.meta.env.VITE_ADMIN_API_KEY;

export const reportingApi = createApi({
  reducerPath: "reportingApi",
  baseQuery: fetchBaseQuery({
    baseUrl: apiBaseUrl,
    prepareHeaders: (headers) => {
      if (ingestApiKey) {
        headers.set("X-Retrospec-Key", ingestApiKey);
      }
      if (adminApiKey) {
        headers.set("X-Retrospec-Admin", adminApiKey);
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
    getIssueStats: builder.query<{ lookbackHours: number; stats: IssueKindStat[] }, number | void>({
      query: (hours) =>
        typeof hours === "number" ? `/v1/issues/stats?hours=${hours}` : "/v1/issues/stats",
      transformResponse: (response: { lookbackHours: number; stats: IssueKindStat[] }) => response,
      providesTags: [{ type: "Issue", id: "STATS" }],
    }),
    getIssueSessions: builder.query<
      {
        clusterKey: string;
        limit: number;
        filters?: { reportStatus: string; minConfidence: number };
        sessions: IssueClusterSession[];
      },
      { clusterKey: string; limit?: number; reportStatus?: string; minConfidence?: number }
    >({
      query: ({ clusterKey, limit, reportStatus, minConfidence }) => {
        const query = new URLSearchParams();
        if (typeof limit === "number") {
          query.set("limit", String(limit));
        }
        if (typeof reportStatus === "string" && reportStatus.trim() !== "") {
          query.set("reportStatus", reportStatus.trim());
        }
        if (typeof minConfidence === "number" && Number.isFinite(minConfidence)) {
          query.set("minConfidence", String(minConfidence));
        }
        const queryString = query.toString();
        return queryString.length > 0
          ? `/v1/issues/${encodeURIComponent(clusterKey)}/sessions?${queryString}`
          : `/v1/issues/${encodeURIComponent(clusterKey)}/sessions`;
      },
      transformResponse: (response: {
        clusterKey: string;
        limit: number;
        filters?: { reportStatus: string; minConfidence: number };
        sessions: IssueClusterSession[];
      }) => response,
      providesTags: (_result, _error, args) => [{ type: "Issue", id: `${args.clusterKey}:sessions` }],
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
    getSessionArtifactToken: builder.query<
      { token: string; expiresAt: string },
      { sessionId: string; artifactType: string }
    >({
      query: ({ sessionId, artifactType }) =>
        `/v1/sessions/${sessionId}/artifacts/${artifactType}/token`,
      transformResponse: (response: { token: string; expiresAt: string }) => response,
      providesTags: (_result, _error, args) => [
        { type: "Session", id: `${args.sessionId}:${args.artifactType}:token` },
      ],
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
        deletedArtifactObjects: number;
        failedEventObjectDelete: number;
        failedArtifactObjectDelete: number;
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
    createProject: builder.mutation<
      {
        project: { id: string; name: string; site: string; createdAt: string };
        apiKey: string;
      },
      { name: string; site: string; label: string }
    >({
      query: (payload) => ({
        url: "/v1/admin/projects",
        method: "POST",
        body: payload,
      }),
      invalidatesTags: [{ type: "Issue", id: "LIST" }],
    }),
    listProjects: builder.query<
      Array<{ id: string; name: string; site: string; createdAt: string }>,
      void
    >({
      query: () => "/v1/admin/projects",
      transformResponse: (response: {
        projects: Array<{ id: string; name: string; site: string; createdAt: string }>;
      }) => response.projects,
    }),
    createProjectKey: builder.mutation<
      {
        apiKeyId: string;
        projectId: string;
        label: string;
        apiKey: string;
      },
      { projectId: string; label: string }
    >({
      query: ({ projectId, label }) => ({
        url: `/v1/admin/projects/${projectId}/keys`,
        method: "POST",
        body: { label },
      }),
    }),
    listProjectKeys: builder.query<
      Array<{
        id: string;
        projectId: string;
        label: string;
        status: string;
        createdAt: string;
        lastUsedAt: string | null;
      }>,
      string
    >({
      query: (projectId) => `/v1/admin/projects/${projectId}/keys`,
      transformResponse: (response: {
        keys: Array<{
          id: string;
          projectId: string;
          label: string;
          status: string;
          createdAt: string;
          lastUsedAt: string | null;
        }>;
      }) => response.keys,
    }),
  }),
});

export const {
  useGetIssuesQuery,
  useGetIssueStatsQuery,
  useGetIssueSessionsQuery,
  useGetSessionQuery,
  useGetSessionEventsQuery,
  useGetSessionArtifactTokenQuery,
  usePromoteIssuesMutation,
  useCleanupDataMutation,
  useCreateProjectMutation,
  useCreateProjectKeyMutation,
  useListProjectsQuery,
  useListProjectKeysQuery,
} = reportingApi;
