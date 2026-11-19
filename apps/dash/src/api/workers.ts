import { useMutation, useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

export type WorkerDetails = Record<
  string,
  {
    has_failed_job: boolean;
    id: string;
    interval: number;
    title: string;
  }
>;

export type WorkerJobLog = {
  created_at: string;
  data?: unknown;
  error?: string;
  id: string;
  name: string;
  status: "done" | "failed" | "started";
  updated_at: string;
};

export type WorkerTemporaryFile = {
  modified_at: string;
  path: string;
  size: string;
};

export type MatchLabResetStatus = {
  id?: string;
  status: "idle" | "started" | "done" | "failed" | "interrupted";
  include_parsing: boolean;
  torrents: number;
  stream_files: number;
  updated_at?: string;
  error?: string;
};

export function useMatchLabResetStatus(workerId: string) {
  return useQuery({
    enabled: workerId === "map-imdb-torrent",
    queryFn: async () => {
      const { data } = await api<MatchLabResetStatus>(
        `/workers/${workerId}/matchlab/reset`,
      );
      return data;
    },
    queryKey: ["/workers/{id}/matchlab/reset", workerId],
    refetchInterval: (query) =>
      query.state.data?.status === "started" ? 2000 : false,
  });
}

export function useWorkerDetails() {
  return useQuery({
    queryFn: getWorkerDetails,
    queryKey: ["/workers/details"],
  });
}

export function useWorkerJobLogs(workerId: string) {
  return useQuery({
    enabled: Boolean(workerId),
    queryFn: () => getWorkerJobLogs(workerId),
    queryKey: ["/workers/{id}/job-logs", workerId],
  });
}

export function useWorkerMutation(workerId: string) {
  const deleteJobLog = useMutation({
    mutationFn: async (jobLogId: string) => {
      await api(`/workers/${workerId}/job-logs/${jobLogId}`, {
        method: "DELETE",
      });
    },
    onSuccess: async (_, jobLogId, __, ctx) => {
      const queryKey = ["/workers/{id}/job-logs", workerId];
      const jobLogs = ctx.client.getQueryData<WorkerJobLog[]>(queryKey);
      if (jobLogs) {
        ctx.client.setQueryData(
          queryKey,
          jobLogs.filter((jobLog) => jobLog.id != jobLogId),
        );
      }
    },
  });

  const purgeJobLogs = useMutation({
    mutationFn: async () => {
      await api(`/workers/${workerId}/job-logs`, { method: "DELETE" });
    },
    onSuccess: async (_, __, ___, ctx) => {
      await ctx.client.invalidateQueries({
        queryKey: ["/workers/{id}/job-logs", workerId],
      });
    },
  });

  const purgeTemporaryFiles = useMutation({
    mutationFn: async () => {
      await api(`/workers/${workerId}/temporary-files`, { method: "DELETE" });
    },
    onSuccess: async (_, __, ___, ctx) => {
      await ctx.client.invalidateQueries({
        queryKey: ["/workers/{id}/temporary-files", workerId],
      });
    },
  });

  const resetProgress = useMutation({
    mutationFn: async () => {
      await api(`/workers/${workerId}/progress`, { method: "DELETE" });
    },
  });

  const resetMatchLab = useMutation({
    mutationFn: async (includeParsing: boolean) => {
      const { data } = await api<MatchLabResetStatus>(
        `/workers/${workerId}/matchlab/reset`,
        {
          method: "POST",
          body: { include_parsing: includeParsing },
        },
      );
      return data;
    },
    onSuccess: async (_, __, ___, ctx) => {
      await ctx.client.invalidateQueries({
        queryKey: ["/workers/{id}/matchlab/reset", workerId],
      });
    },
  });

  return { deleteJobLog, purgeJobLogs, purgeTemporaryFiles, resetProgress, resetMatchLab };
}

export function useWorkerTemporaryFiles(workerId: string) {
  return useQuery({
    enabled: Boolean(workerId),
    queryFn: async () => {
      const { data } = await api<WorkerTemporaryFile[]>(
        `/workers/${workerId}/temporary-files`,
      );
      return data;
    },
    queryKey: ["/workers/{id}/temporary-files", workerId],
  });
}

async function getWorkerDetails() {
  const { data } = await api<WorkerDetails>("/workers/details");
  return data;
}

async function getWorkerJobLogs(workerId: string) {
  const { data } = await api<WorkerJobLog[]>(`/workers/${workerId}/job-logs`);
  return data;
}
