import { createFileRoute } from "@tanstack/react-router";
import { ColumnDef } from "@tanstack/react-table";
import { Trash2 } from "lucide-react";
import { DateTime, Duration } from "luxon";
import { useEffect, useMemo, useState } from "react";
import { useLocalStorage } from "react-use";
import { toast } from "sonner";

import {
  MatchLabResetStatus,
  useMatchLabResetStatus,
  useWorkerDetails,
  useWorkerJobLogs,
  useWorkerMutation,
  useWorkerTemporaryFiles,
  WorkerJobLog,
} from "@/api/workers";
import { DataTable } from "@/components/data-table";
import { useDataTable } from "@/components/data-table/use-data-table";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemFooter,
  ItemGroup,
  ItemTitle,
} from "@/components/ui/item";
import { Label } from "@/components/ui/label";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { APIError } from "@/lib/api";

declare module "@/components/data-table" {
  export interface DataTableMetaCtx {
    WorkerJobLog: {
      deleteJobLog: ReturnType<typeof useWorkerMutation>["deleteJobLog"];
    };
  }

  export interface DataTableMetaCtxKey {
    WorkerJobLog: WorkerJobLog;
  }
}

const jobLogsColumns: ColumnDef<WorkerJobLog>[] = [
  {
    accessorKey: "id",
    header: "ID",
  },
  {
    accessorKey: "created_at",
    cell: ({ getValue }) => {
      const date = DateTime.fromISO(getValue<string>());
      return date.toLocaleString(DateTime.DATETIME_MED_WITH_SECONDS);
    },
    header: "Started At",
  },
  {
    accessorKey: "status",
    cell: ({ getValue }) => {
      const status = getValue<string>();
      const colors = {
        done: "text-green-500",
        failed: "text-red-500",
        started: "text-cyan-500",
      };
      return (
        <span className={colors[status as keyof typeof colors] || ""}>
          {status}
        </span>
      );
    },
    header: "Status",
  },
  {
    accessorKey: "updated_at",
    cell: ({ getValue }) => {
      const date = DateTime.fromISO(getValue<string>());
      return date.toLocaleString(DateTime.DATETIME_MED_WITH_SECONDS);
    },
    header: "Last Heartbeat At",
  },
  {
    accessorKey: "error",
    cell: ({ getValue }) => {
      const error = getValue<string | undefined>();
      return error ? (
        <span className="font-mono text-xs text-red-600">{error}</span>
      ) : (
        "-"
      );
    },
    header: "Error",
  },
  {
    cell: (c) => {
      const { deleteJobLog } = c.table.options.meta!.ctx;
      return (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                disabled={deleteJobLog.isPending}
                onClick={() => {
                  toast.promise(deleteJobLog.mutateAsync(c.row.original.id), {
                    error(err: APIError) {
                      console.error(err);
                      return {
                        closeButton: true,
                        message: err.message,
                      };
                    },
                    loading: "Deleting Job Log...",
                    success: {
                      closeButton: true,
                      message: "Job Log Deleted!",
                    },
                  });
                }}
                size="icon-sm"
                variant="ghost"
              >
                <Trash2 />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Delete Job Log</TooltipContent>
          </Tooltip>
        </>
      );
    },
    header: "",
    id: "actions",
  },
];

const canPurgeTemporaryDataByWorkerId: Record<string, boolean> = {
  "sync-animetosho": true,
  "sync-imdb": true,
};

const canResetProgressByWorkerId: Record<string, boolean> = {
  "sync-bitmagnet": true,
  "sync-dmm-hashlist": true,
};

function PurgeWorkerTemporaryDataButton({
  mutation,
  workerId,
}: {
  mutation: ReturnType<typeof useWorkerMutation>["purgeTemporaryFiles"];
  workerId: string;
}) {
  workerId = canPurgeTemporaryDataByWorkerId[workerId] ? workerId : "";

  const files = useWorkerTemporaryFiles(workerId);

  if (!workerId) {
    return null;
  }

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button disabled={mutation.isPending} size="sm" variant="destructive">
          Purge Temporary Files
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Are you sure?</AlertDialogTitle>
          <AlertDialogDescription>
            This will delete all the temporary files for this worker.
            <ScrollArea className="h-full max-h-52">
              <ItemGroup className="mt-2 gap-2">
                {files.data?.map((item) => (
                  <Item key={item.path} size="sm" variant="muted">
                    <ItemContent>
                      <ItemTitle>
                        <strong>{item.path}</strong>
                      </ItemTitle>
                      <ItemDescription className="flex justify-between">
                        <em>Size:</em> {item.size}
                      </ItemDescription>
                      <ItemFooter>
                        <em>Modified At:</em>{" "}
                        {DateTime.fromISO(item.modified_at).toLocaleString(
                          DateTime.DATETIME_MED_WITH_SECONDS,
                        )}
                      </ItemFooter>
                    </ItemContent>
                  </Item>
                ))}
              </ItemGroup>
              <ScrollBar orientation="vertical" />
            </ScrollArea>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction asChild>
            <Button
              disabled={mutation.isPending}
              onClick={async () => {
                toast.promise(mutation.mutateAsync(), {
                  error(err: APIError) {
                    console.error(err);
                    return {
                      closeButton: true,
                      message: err.message,
                    };
                  },
                  loading: "Purging Temporary Files...",
                  success: {
                    closeButton: true,
                    message: "Temporary Files Purged!",
                  },
                });
              }}
              variant="destructive"
            >
              Purge
            </Button>
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function ResetWorkerProgressButton({
  mutation,
  workerId,
}: {
  mutation: ReturnType<typeof useWorkerMutation>["resetProgress"];
  workerId: string;
}) {
  if (!canResetProgressByWorkerId[workerId]) {
    return null;
  }

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button disabled={mutation.isPending} size="sm" variant="destructive">
          Reset Progress
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Are you sure?</AlertDialogTitle>
          <AlertDialogDescription>
            This will reset the sync progress for this worker. The next run will
            start from scratch.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction asChild>
            <Button
              disabled={mutation.isPending}
              onClick={async () => {
                toast.promise(mutation.mutateAsync(), {
                  error(err: APIError) {
                    console.error(err);
                    return {
                      closeButton: true,
                      message: err.message,
                    };
                  },
                  loading: "Resetting Progress...",
                  success: {
                    closeButton: true,
                    message: "Progress Reset!",
                  },
                });
              }}
              variant="destructive"
            >
              Reset
            </Button>
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function ResetMatchLabButton({
  mutation,
  enabled,
  status,
}: {
  mutation: ReturnType<typeof useWorkerMutation>["resetMatchLab"];
  enabled: boolean;
  status?: MatchLabResetStatus;
}) {
  const [includeParsing, setIncludeParsing] = useState(false);
  if (!enabled) {
    return null;
  }

  const running = status?.status === "started";

  return (
    <div className="flex flex-col items-end gap-1">
      <AlertDialog>
        <AlertDialogTrigger asChild>
          <Button
            disabled={mutation.isPending || running}
            size="sm"
            variant="destructive"
          >
            {running ? "Reset in progress..." : "Reset MatchLab Mapping"}
          </Button>
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Reset all MatchLab mappings?</AlertDialogTitle>
            <AlertDialogDescription>
              This only resets MatchLab processing status for stored torrents.
              Existing IMDb/AniDB mappings and stream-file SID/ASID values stay
              in place until the normal MatchLab worker replaces them. The
              operation runs in the background and its status is shown here.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-start gap-3">
            <Checkbox
              checked={includeParsing}
              id="matchlab-reset-parsing"
              onCheckedChange={(checked) => setIncludeParsing(checked === true)}
            />
            <Label htmlFor="matchlab-reset-parsing">
              Also reset parsing (reparse all torrent titles before rematching)
            </Label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction asChild>
              <Button
                disabled={mutation.isPending || running}
                onClick={() => {
                  toast.promise(mutation.mutateAsync(includeParsing), {
                    error(err: APIError) {
                      console.error(err);
                      return { closeButton: true, message: err.message };
                    },
                    loading: "Starting MatchLab reset...",
                    success: {
                      closeButton: true,
                      message: "Reset started. Progress is shown in Workers.",
                    },
                  });
                }}
                variant="destructive"
              >
                Start reset
              </Button>
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {running && (
        <span className="text-muted-foreground text-xs">
          Reset is running. Refreshing status automatically...
        </span>
      )}
      {status?.status === "done" && (
        <span className="text-muted-foreground text-xs">
          Last reset completed: {status.torrents.toLocaleString()} torrents queued for rematching.
        </span>
      )}
      {(status?.status === "failed" || status?.status === "interrupted") && (
        <span className="text-destructive text-xs">
          Reset {status.status}: {status.error || "check server logs"}
        </span>
      )}
    </div>
  );
}

export const Route = createFileRoute("/dash/workers")({
  component: RouteComponent,
  staticData: {
    crumb: "Workers",
  },
});

function RouteComponent() {
  const workerDetails = useWorkerDetails();

  const [selectedWorkerId = "", setSelectedWorkerId] = useLocalStorage(
    "dash/workers:selected-worker-id",
    "",
  );

  const jobLogs = useWorkerJobLogs(selectedWorkerId);
  const matchLabReset = useMatchLabResetStatus(selectedWorkerId);
  const { deleteJobLog, purgeJobLogs, purgeTemporaryFiles, resetProgress, resetMatchLab } =
    useWorkerMutation(selectedWorkerId);

  const workerOptions = useMemo(() => {
    return Object.entries(workerDetails.data ?? {})
      .map(([value, details]) => ({
        indicator: details.has_failed_job ? `❗` : "",
        label: details.title,
        value,
      }))
      .sort((a, b) => a.label.localeCompare(b.label))
      .map((o) => ({
        ...o,
        label: `${o.indicator ? `${o.indicator} ` : ""}${o.label}`,
      }));
  }, [workerDetails.data]);

  useEffect(() => {
    setSelectedWorkerId((workerId) => {
      if (workerId || !workerOptions.length) {
        return workerId;
      }
      return workerOptions[0].value;
    });
  }, [setSelectedWorkerId, workerOptions]);

  const selectedWorkerInterval = useMemo(() => {
    const worker = workerDetails.data?.[selectedWorkerId];
    if (!worker) {
      return "";
    }
    return Duration.fromMillis(worker.interval / 1000 / 1000)
      .shiftTo("months", "days", "hours", "minutes", "seconds")
      .removeZeros()
      .toHuman({ maximumFractionDigits: 0 });
  }, [selectedWorkerId, workerDetails.data]);

  const table = useDataTable({
    columns: jobLogsColumns,
    data: jobLogs.data ?? [],
    initialState: {
      columnPinning: { left: ["id"], right: ["actions"] },
    },
    meta: { ctx: { deleteJobLog } },
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-4">
        <Label className="text-sm font-medium" htmlFor="worker">
          Worker:
        </Label>
        {workerDetails.isLoading ? (
          <div className="text-muted-foreground text-sm">
            Loading workers...
          </div>
        ) : workerDetails.isError ? (
          <div className="text-sm text-red-600">Error loading workers</div>
        ) : (
          <Select onValueChange={setSelectedWorkerId} value={selectedWorkerId}>
            <SelectTrigger className="w-[300px]" id="worker">
              <SelectValue placeholder="Select worker" />
            </SelectTrigger>
            <SelectContent>
              {workerOptions.map(({ label, value }) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <div>
          {selectedWorkerInterval && (
            <div>Interval: {selectedWorkerInterval}</div>
          )}
        </div>
      </div>

      <div>
        <div className="mb-4 flex flex-row flex-wrap items-center justify-between">
          <h3 className="font-semibold">Job Logs</h3>
          <div className="flex flex-row flex-wrap gap-2">
            <PurgeWorkerTemporaryDataButton
              mutation={purgeTemporaryFiles}
              workerId={selectedWorkerId}
            />
            <ResetWorkerProgressButton
              mutation={resetProgress}
              workerId={selectedWorkerId}
            />
            <ResetMatchLabButton
              enabled={selectedWorkerId === "map-imdb-torrent" &&
                workerDetails.data?.[selectedWorkerId]?.title === "Map IMDb/AniDB via MatchLab"}
              mutation={resetMatchLab}
              status={matchLabReset.data}
            />
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  disabled={purgeJobLogs.isPending}
                  size="sm"
                  variant="destructive"
                >
                  Purge Job Logs
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Are you sure?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This will delete all the job logs below.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction asChild>
                    <Button
                      onClick={() => {
                        toast.promise(purgeJobLogs.mutateAsync(), {
                          error(err: APIError) {
                            console.error(err);
                            return {
                              closeButton: true,
                              message: err.message,
                            };
                          },
                          loading: "Purging Job Logs...",
                          success: {
                            closeButton: true,
                            message: "Job Logs Purged!",
                          },
                        });
                      }}
                      variant="destructive"
                    >
                      Purge
                    </Button>
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </div>
        {selectedWorkerId &&
          (jobLogs.isLoading ? (
            <div className="text-muted-foreground text-sm">
              Loading job logs...
            </div>
          ) : jobLogs.isError ? (
            <div className="text-sm text-red-600">Error loading job logs</div>
          ) : (
            <DataTable table={table} />
          ))}
      </div>
    </div>
  );
}
