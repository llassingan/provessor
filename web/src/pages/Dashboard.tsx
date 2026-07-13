import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { vps } from "../lib/api";
import type { VPS, Settings } from "../lib/api";
import VPSActions from "../components/VPSActions";

interface DashboardProps {
  settings: Settings | null;
  onSettingsRefresh: () => void;
}

function StatusBadge({ status }: { status: VPS["status"] }): JSX.Element {
  const styles: Record<string, string> = {
    pending: "bg-gray-100 text-gray-700",
    provisioning: "bg-blue-100 text-blue-700",
    running: "bg-emerald-100 text-emerald-700",
    stopped: "bg-amber-100 text-amber-700",
    restarting: "bg-emerald-100 text-emerald-700",
    resetting: "bg-red-100 text-red-700",
    failed: "bg-red-100 text-red-700",
    terminating: "bg-orange-100 text-orange-700",
    terminated: "bg-gray-100 text-gray-500 line-through",
  };

  const labels: Record<string, string> = {
    pending: "Pending",
    provisioning: "Provisioning",
    running: "Running",
    stopped: "Stopped",
    restarting: "Restarting",
    resetting: "Resetting",
    failed: "Failed",
    terminating: "Terminating",
    terminated: "Terminated",
  };

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${styles[status] ?? "bg-gray-100 text-gray-700"}`}
    >
      {status === "provisioning" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
      )}
      {status === "restarting" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" />
      )}
      {status === "resetting" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-red-500" />
      )}
      {status === "terminating" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-orange-500" />
      )}
      {labels[status] ?? status}
    </span>
  );
}

function SkeletonCard(): JSX.Element {
  return (
    <div className="animate-pulse rounded-xl border border-gray-200 bg-white p-5">
      <div className="mb-3 h-4 w-1/2 rounded bg-gray-200" />
      <div className="mb-3 h-3 w-1/3 rounded bg-gray-200" />
      <div className="mb-4 h-3 w-2/3 rounded bg-gray-100" />
      <div className="flex gap-3">
        <div className="h-6 w-14 rounded bg-gray-200" />
        <div className="h-6 w-14 rounded bg-gray-200" />
      </div>
    </div>
  );
}

export default function Dashboard({
  settings,
  onSettingsRefresh: _onSettingsRefresh,
}: DashboardProps): JSX.Element {
  const [instances, setInstances] = useState<VPS[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const navigate = useNavigate();

  const fetchInstances = useCallback((showLoading = true) => {
    if (showLoading) {
      setLoading(true);
      setError("");
    }
    vps
      .list()
      .then((data) => {
        setInstances(data);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load instances",
        );
      })
      .finally(() => {
        if (showLoading) {
          setLoading(false);
        }
      });
  }, []);

  useEffect(() => {
    fetchInstances();
  }, [fetchInstances]);

  useEffect(() => {
    if (!instances.some((inst) => inst.status === "terminating")) return;

    const interval = window.setInterval(() => {
      fetchInstances(false);
    }, 5000);

    return () => {
      window.clearInterval(interval);
    };
  }, [fetchInstances, instances]);

  const handleUpdate = (updated: VPS): void => {
    setInstances((prev) =>
      prev.map((inst) => (inst.id === updated.id ? updated : inst)),
    );
  };

  const handleDelete = (deletedId: number): void => {
    setInstances((prev) => prev.filter((inst) => inst.id !== deletedId));
  };

  const hasCredentials = settings !== null && settings.tenancy_ocid !== "";
  const networkReady = hasCredentials;

  return (
    <div>
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          <p className="mt-1 text-sm text-gray-500">
            Manage your VPS instances
          </p>
        </div>
        <div className="flex gap-3">
          <button
            type="button"
            onClick={() => {
              fetchInstances();
            }}
            className="rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50"
          >
            Refresh
          </button>
          <Link
            to={networkReady ? "/vps/new" : "/settings"}
            className={`inline-flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
              networkReady
                ? "bg-primary-600 text-white hover:bg-primary-700"
                : "bg-amber-600 text-white hover:bg-amber-700"
            }`}
          >
            {networkReady ? "+ New VPS" : "Set up credentials"}
          </Link>
        </div>
      </div>

      {!networkReady && (
        <div className="mb-6 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
          Network not configured.{" "}
          <Link
            to="/settings"
            className="font-medium underline underline-offset-2"
          >
            Go to Settings
          </Link>{" "}
          to complete setup before creating a VPS.
        </div>
      )}

      {error && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
          <button
            type="button"
            onClick={() => {
              fetchInstances();
            }}
            className="ml-3 font-medium underline underline-offset-2"
          >
            Retry
          </button>
        </div>
      )}

      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <SkeletonCard key={i} />
          ))}
        </div>
      ) : instances.length === 0 ? (
        <div className="rounded-xl border border-dashed border-gray-300 bg-white p-12 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100">
            <svg
              className="h-6 w-6 text-gray-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={1.5}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z"
              />
            </svg>
          </div>
          <h3 className="text-sm font-semibold text-gray-900">
            No VPS instances yet
          </h3>
          <p className="mt-1 text-sm text-gray-500">
            Create your first one to get started.
          </p>
          {networkReady && (
            <Link
              to="/vps/new"
              className="mt-4 inline-block rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white hover:bg-primary-700"
            >
              Create VPS
            </Link>
          )}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {instances.map((inst) => (
            <button
              key={inst.id}
              type="button"
              onClick={() => {
                navigate(`/vps/${inst.id}`);
              }}
              className="group relative w-full cursor-pointer rounded-xl border border-gray-200 bg-white p-5 text-left shadow-sm transition-all hover:border-primary-300 hover:shadow-md"
            >
              <div className="mb-3 flex items-start justify-between">
                <div>
                  <h3 className="font-semibold text-gray-900 group-hover:text-primary-600">
                    {inst.display_name}
                  </h3>
                  <p className="mt-0.5 text-xs text-gray-400">
                    Created {new Date(inst.created_at).toLocaleDateString()}
                  </p>
                </div>
                <StatusBadge status={inst.status} />
              </div>

              <div className="mb-4 space-y-1 text-sm text-gray-600">
                {inst.public_ip && (
                  <div className="flex items-center gap-1.5">
                    <span className="text-gray-400">IP:</span>
                    <code className="rounded bg-gray-100 px-1.5 py-0.5 text-xs">
                      {inst.public_ip}
                    </code>
                    <span
                      role="button"
                      tabIndex={0}
                      onClick={(e) => {
                        e.stopPropagation();
                        void navigator.clipboard.writeText(
                          inst.public_ip ?? "",
                        );
                      }}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.stopPropagation();
                          void navigator.clipboard.writeText(inst.public_ip ?? '');
                        }
                      }}
                      className="rounded p-0.5 text-gray-400 hover:text-gray-600"
                      title="Copy IP"
                    >
                      <svg
                        className="h-3.5 w-3.5"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth={1.5}
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184"
                        />
                      </svg>
                    </span>
                  </div>
                )}
                <div className="text-xs text-gray-400">
                  {inst.ocpu} OCPU &middot; {inst.memory_gb} GB RAM &middot;{" "}
                  {inst.boot_volume_size_gb} GB
                </div>
              </div>

              <div
                onClick={(e) => { e.stopPropagation(); }}
                onKeyDown={(e) => { e.stopPropagation(); }}
                className="border-t border-gray-100 pt-3"
                role="presentation"
              >
                <VPSActions
                  vpsInstance={inst}
                  onUpdate={handleUpdate}
                  onDelete={() => {
                    handleDelete(inst.id);
                  }}
                />
              </div>

              <div className="mt-2 flex items-center gap-1 text-xs text-gray-300 transition-colors group-hover:text-primary-500">
                <span className="hidden group-hover:inline">View details</span>
                <svg className="ml-auto h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
                </svg>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
