import { useState } from "react";
import type { VPS } from "../lib/api";
import { vps } from "../lib/api";

interface VPSActionsProps {
  vpsInstance: VPS;
  onUpdate: (updated: VPS) => void;
  onDelete?: () => void;
}

export default function VPSActions({
  vpsInstance,
  onUpdate,
  onDelete,
}: VPSActionsProps): JSX.Element {
  const [loading, setLoading] = useState<string | null>(null);
  const [confirmTerminate, setConfirmTerminate] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const status = vpsInstance.status;
  const isRunning = status === "running";
  const isStopped = status === "stopped";
  const isTerminated = status === "terminated";
  const isFailed = status === "failed";
  const isPending = status === "pending" || status === "provisioning";

  const handleAction = async (action: "start" | "stop"): Promise<void> => {
    setLoading(action);
    try {
      const updated = await vps.action(vpsInstance.id, action);
      onUpdate(updated);
    } catch (err) {
      alert(
        err instanceof Error ? err.message : "Action failed",
      );
    } finally {
      setLoading(null);
    }
  };

  const handleTerminate = async (): Promise<void> => {
    setLoading("terminate");
    try {
      await vps.terminate(vpsInstance.id);
      onUpdate({ ...vpsInstance, status: "terminated" });
    } catch (err) {
      alert(
        err instanceof Error ? err.message : "Terminate failed",
      );
    } finally {
      setLoading(null);
      setConfirmTerminate(false);
    }
  };

  const handleDelete = async (): Promise<void> => {
    setLoading("delete");
    try {
      await vps.delete(vpsInstance.id);
      if (onDelete) {
        onDelete();
      } else {
        window.location.href = "/dashboard";
      }
    } catch (err) {
      alert(
        err instanceof Error ? err.message : "Delete failed",
      );
      setLoading(null);
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      {isStopped && (
        <button
          type="button"
          onClick={() => {
            void handleAction("start");
          }}
          disabled={loading !== null}
          className="inline-flex items-center gap-1.5 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-emerald-700 disabled:opacity-50"
        >
          {loading === "start" ? "Starting..." : "Start"}
        </button>
      )}

      {isRunning && (
        <button
          type="button"
          onClick={() => {
            void handleAction("stop");
          }}
          disabled={loading !== null}
          className="inline-flex items-center gap-1.5 rounded-lg bg-amber-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-amber-700 disabled:opacity-50"
        >
          {loading === "stop" ? "Stopping..." : "Stop"}
        </button>
      )}

      {(isRunning || isStopped || isFailed) && !isTerminated && (
        confirmTerminate ? (
          <div className="flex items-center gap-2">
            <span className="text-sm text-orange-600">Terminate instance?</span>
            <button
              type="button"
              onClick={() => {
                void handleTerminate();
              }}
              disabled={loading !== null}
              className="rounded-lg bg-orange-600 px-3 py-2 text-sm font-medium text-white hover:bg-orange-700 disabled:opacity-50"
            >
              {loading === "terminate" ? "Terminating..." : "Yes, terminate"}
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirmTerminate(false);
              }}
              className="rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => {
              setConfirmTerminate(true);
            }}
            disabled={loading !== null}
            className="inline-flex items-center gap-1.5 rounded-lg border border-orange-200 bg-white px-4 py-2 text-sm font-medium text-orange-600 transition-colors hover:bg-orange-50"
          >
            Terminate
          </button>
        )
      )}

      {(isTerminated || isFailed || isPending) && (
        confirmDelete ? (
          <div className="flex items-center gap-2">
            <span className="text-sm text-red-600">Delete record?</span>
            <button
              type="button"
              onClick={() => {
                void handleDelete();
              }}
              disabled={loading !== null}
              className="rounded-lg bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
            >
              {loading === "delete" ? "Deleting..." : "Yes, delete"}
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirmDelete(false);
              }}
              className="rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Cancel
            </button>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => {
              setConfirmDelete(true);
            }}
            disabled={loading !== null}
            className="inline-flex items-center gap-1.5 rounded-lg border border-red-200 bg-white px-4 py-2 text-sm font-medium text-red-600 transition-colors hover:bg-red-50"
          >
            Delete
          </button>
        )
      )}
    </div>
  );
}
