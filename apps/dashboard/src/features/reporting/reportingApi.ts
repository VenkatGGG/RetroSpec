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

export interface QueueHealthSnapshot {
  status: "healthy" | "warning" | "critical";
  generatedAt: string;
  replay: {
    streamDepth: number;
    pending: number;
    retryDepth: number;
    failedDepth: number;
  };
  analysis: {
    streamDepth: number;
    pending: number;
    retryDepth: number;
    failedDepth: number;
  };
  thresholds: {
    warningPending: number;
    warningRetry: number;
    criticalPending: number;
    criticalRetry: number;
    criticalFailed: number;
  };
}

export interface QueueRedriveResult {
  queueKind: "replay" | "analysis";
  requested: number;
  redriven: number;
  skipped: number;
  remainingFailed: number;
}

export interface QueueDeadLetterEntry {
  failedAt: string;
  error: string;
  attempt: number;
  projectId: string;
  sessionId: string;
  triggerKind: string;
  route: string;
  site: string;
  payload: string;
  raw: string;
}

export interface QueueDeadLetterListResult {
  queueKind: "replay" | "analysis";
  limit: number;
  total: number;
  unparsable: number;
  entries: QueueDeadLetterEntry[];
}

export interface IssueFeedbackEvent {
  id: string;
  projectId: string;
  clusterKey: string;
  sessionId?: string;
  feedbackKind:
    | "false_positive"
    | "true_positive"
    | "invalid"
    | "suppressed"
    | "unsuppressed"
    | "merge"
    | "split";
  note: string;
  metadata?: Record<string, unknown>;
  createdBy: string;
  createdAt: string;
}

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
    getIssues: builder.query<IssueCluster[], string | void>({
      query: (stateFilter) =>
        stateFilter && stateFilter.trim().length > 0
          ? `/v1/issues?state=${encodeURIComponent(stateFilter.trim())}`
          : "/v1/issues",
      transformResponse: (response: { issues: IssueCluster[]; state?: string }) => response.issues,
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
    listIssueFeedback: builder.query<
      {
        clusterKey: string;
        limit: number;
        events: IssueFeedbackEvent[];
      },
      { clusterKey: string; limit?: number }
    >({
      query: ({ clusterKey, limit }) =>
        typeof limit === "number"
          ? `/v1/issues/${encodeURIComponent(clusterKey)}/feedback?limit=${limit}`
          : `/v1/issues/${encodeURIComponent(clusterKey)}/feedback`,
      transformResponse: (response: {
        clusterKey: string;
        limit: number;
        events: IssueFeedbackEvent[];
      }) => response,
      providesTags: (_result, _error, args) => [{ type: "Issue", id: `${args.clusterKey}:feedback` }],
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
    updateIssueState: builder.mutation<
      {
        state: {
          projectId: string;
          clusterKey: string;
          state: "open" | "acknowledged" | "resolved" | "muted";
          assignee: string;
          mutedUntil: string | null;
          note: string;
          createdAt: string;
          updatedAt: string;
        };
      },
      {
        clusterKey: string;
        state: "open" | "acknowledged" | "resolved" | "muted";
        assignee?: string;
        mutedUntil?: string;
        note?: string;
      }
    >({
      query: ({ clusterKey, ...body }) => ({
        url: `/v1/issues/${encodeURIComponent(clusterKey)}/state`,
        method: "POST",
        body,
      }),
      invalidatesTags: (_result, _error, args) => [
        { type: "Issue", id: "LIST" },
        { type: "Issue", id: args.clusterKey },
        { type: "Issue", id: `${args.clusterKey}:sessions` },
      ],
    }),
    submitIssueFeedback: builder.mutation<
      { feedback: IssueFeedbackEvent },
      {
        clusterKey: string;
        kind: IssueFeedbackEvent["feedbackKind"];
        sessionId?: string;
        note?: string;
        createdBy?: string;
        metadata?: Record<string, unknown>;
      }
    >({
      query: ({ clusterKey, ...body }) => ({
        url: `/v1/issues/${encodeURIComponent(clusterKey)}/feedback`,
        method: "POST",
        body,
      }),
      invalidatesTags: (_result, _error, args) => [
        { type: "Issue", id: "LIST" },
        { type: "Issue", id: args.clusterKey },
        { type: "Issue", id: `${args.clusterKey}:feedback` },
      ],
    }),
    mergeIssues: builder.mutation<
      {
        result: {
          targetClusterKey: string;
          sourceClusterKeys: string[];
          movedMarkerCount: number;
        };
      },
      { targetClusterKey: string; sourceClusterKeys: string[]; note?: string; createdBy?: string }
    >({
      query: (payload) => ({
        url: "/v1/issues/merge",
        method: "POST",
        body: payload,
      }),
      invalidatesTags: [{ type: "Issue", id: "LIST" }],
    }),
    splitIssue: builder.mutation<
      {
        result: {
          sourceClusterKey: string;
          newClusterKey: string;
          movedMarkerCount: number;
        };
      },
      {
        clusterKey: string;
        newClusterKey?: string;
        sessionIds: string[];
        note?: string;
        createdBy?: string;
      }
    >({
      query: ({ clusterKey, ...body }) => ({
        url: `/v1/issues/${encodeURIComponent(clusterKey)}/split`,
        method: "POST",
        body,
      }),
      invalidatesTags: (_result, _error, args) => [
        { type: "Issue", id: "LIST" },
        { type: "Issue", id: args.clusterKey },
        { type: "Issue", id: `${args.clusterKey}:sessions` },
        { type: "Issue", id: `${args.clusterKey}:feedback` },
      ],
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
    getQueueHealth: builder.query<QueueHealthSnapshot, void>({
      query: () => "/v1/admin/queue-health",
      transformResponse: (response: QueueHealthSnapshot) => response,
    }),
    getQueueDeadLetters: builder.query<
      QueueDeadLetterListResult,
      { queue: "replay" | "analysis"; limit?: number }
    >({
      query: ({ queue, limit }) => {
        const query = new URLSearchParams();
        query.set("queue", queue);
        if (typeof limit === "number") {
          query.set("limit", String(limit));
        }
        return `/v1/admin/queue-dead-letters?${query.toString()}`;
      },
      transformResponse: (response: { result: QueueDeadLetterListResult }) => response.result,
    }),
    redriveQueueDeadLetters: builder.mutation<
      { result: QueueRedriveResult },
      { queue: "replay" | "analysis"; limit?: number }
    >({
      query: ({ queue, limit }) => ({
        url: "/v1/admin/queue-redrive",
        method: "POST",
        body: {
          queue,
          limit,
        },
      }),
    }),
  }),
});

export const {
  useGetIssuesQuery,
  useGetIssueStatsQuery,
  useGetIssueSessionsQuery,
  useListIssueFeedbackQuery,
  useGetSessionQuery,
  useGetSessionEventsQuery,
  useGetSessionArtifactTokenQuery,
  usePromoteIssuesMutation,
  useUpdateIssueStateMutation,
  useSubmitIssueFeedbackMutation,
  useMergeIssuesMutation,
  useSplitIssueMutation,
  useCleanupDataMutation,
  useCreateProjectMutation,
  useCreateProjectKeyMutation,
  useGetQueueHealthQuery,
  useGetQueueDeadLettersQuery,
  useRedriveQueueDeadLettersMutation,
  useListProjectsQuery,
  useListProjectKeysQuery,
} = reportingApi;
