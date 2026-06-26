import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { networks } from "../lib/api";
import type { Network } from "../lib/api";

function StatusBadge({ status }: { status: Network["status"] }): JSX.Element {
  const styles: Record<string, string> = {
    pending: "bg-gray-100 text-gray-700",
    provisioning: "bg-amber-100 text-amber-700",
    ready: "bg-emerald-100 text-emerald-700",
    failed: "bg-red-100 text-red-700",
  };

  const labels: Record<string, string> = {
    pending: "Pending",
    provisioning: "Provisioning",
    ready: "Ready",
    failed: "Failed",
  };

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${styles[status] ?? "bg-gray-100 text-gray-700"}`}
    >
      {status === "provisioning" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-500" />
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

export default function Networks(): JSX.Element {
  const [networkList, setNetworkList] = useState<Network[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [provisioningIds, setProvisioningIds] = useState<Set<number>>(
    new Set(),
  );
  const navigate = useNavigate();

  const fetchNetworks = useCallback(() => {
    setLoading(true);
    setError("");
    networks
      .list()
      .then((data) => {
        setNetworkList(data);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load networks",
        );
      })
      .finally(() => {
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    fetchNetworks();
  }, [fetchNetworks]);

  const handleProvision = useCallback(
    async (id: number) => {
      try {
        await networks.provision(id);
      } catch (err: unknown) {
        setError(
          err instanceof Error ? err.message : "Provision failed",
        );
        return;
      }

      setProvisioningIds((prev) => new Set(prev).add(id));

      const poll = setInterval(async () => {
        try {
          const data = await networks.list();
          setNetworkList(data);
          const updated = data.find((n) => n.id === id);
          if (updated && updated.status !== "provisioning") {
            clearInterval(poll);
            setProvisioningIds((prev) => {
              const next = new Set(prev);
              next.delete(id);
              return next;
            });
          }
        } catch {
          clearInterval(poll);
          setProvisioningIds((prev) => {
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
        }
      }, 2000);
    },
    [],
  );

  const handleDelete = useCallback(
    async (id: number) => {
      if (!window.confirm("Delete this network permanently?")) return;
      try {
        await networks.delete(id);
        setNetworkList((prev) => prev.filter((n) => n.id !== id));
      } catch (err: unknown) {
        setError(
          err instanceof Error ? err.message : "Failed to delete network",
        );
      }
    },
    [],
  );

  return (
    <div>
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Networks</h1>
          <p className="mt-1 text-sm text-gray-500">
            Manage your VCN and subnet configurations
          </p>
        </div>
        <div className="flex gap-3">
          <button
            type="button"
            onClick={fetchNetworks}
            className="rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50"
          >
            Refresh
          </button>
          <Link
            to="/networks/new"
            className="inline-flex items-center gap-1.5 rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-700"
          >
            + Create Network
          </Link>
        </div>
      </div>

      {error && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
          <button
            type="button"
            onClick={fetchNetworks}
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
      ) : networkList.length === 0 ? (
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
                d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418"
              />
            </svg>
          </div>
          <h3 className="text-sm font-semibold text-gray-900">
            No networks yet
          </h3>
          <p className="mt-1 text-sm text-gray-500">
            Create your first network to get started.
          </p>
          <Link
            to="/networks/new"
            className="mt-4 inline-block rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white hover:bg-primary-700"
          >
            Create your first network
          </Link>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {networkList.map((net) => (
            <div
              key={net.id}
              className="group rounded-xl border border-gray-200 bg-white p-5 shadow-sm transition-shadow hover:shadow-md"
            >
              <div className="mb-3 flex items-start justify-between">
                <button
                  type="button"
                  onClick={() => {
                    navigate(`/networks/${net.id}`);
                  }}
                  className="text-left"
                >
                  <h3 className="font-semibold text-gray-900 hover:text-primary-600">
                    {net.name}
                  </h3>
                  <p className="mt-0.5 text-xs text-gray-400">
                    Created {new Date(net.created_at).toLocaleDateString()}
                  </p>
                </button>
                <StatusBadge status={net.status} />
              </div>

              <div className="mb-4 space-y-1.5 text-sm">
                <div className="flex items-center gap-1.5">
                  <span className="text-gray-400">VCN:</span>
                  <code className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-700">
                    {net.cidr_vcn}
                  </code>
                </div>
                <div className="flex items-center gap-1.5">
                  <span className="text-gray-400">Subnet:</span>
                  <code className="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-700">
                    {net.cidr_subnet}
                  </code>
                </div>
                {net.status === "ready" && (
                  <div className="pt-1 text-xs text-gray-500">
                    N/A VPS
                  </div>
                )}
              </div>

              <div className="flex items-center gap-2 border-t border-gray-100 pt-3">
                {net.status !== "ready" && (
                  <button
                    type="button"
                    onClick={() => {
                      void handleProvision(net.id);
                    }}
                    disabled={provisioningIds.has(net.id)}
                    className={`inline-flex items-center gap-1 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                      provisioningIds.has(net.id)
                        ? "cursor-not-allowed bg-amber-100 text-amber-600"
                        : "bg-amber-600 text-white hover:bg-amber-700"
                    }`}
                  >
                    {provisioningIds.has(net.id) ? (
                      <>
                        <svg
                          className="h-3 w-3 animate-spin"
                          fill="none"
                          viewBox="0 0 24 24"
                        >
                          <circle
                            className="opacity-25"
                            cx="12"
                            cy="12"
                            r="10"
                            stroke="currentColor"
                            strokeWidth="4"
                          />
                          <path
                            className="opacity-75"
                            fill="currentColor"
                            d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z"
                          />
                        </svg>
                        Provisioning
                      </>
                    ) : (
                      "Provision"
                    )}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => {
                    void handleDelete(net.id);
                  }}
                  disabled={net.status === "provisioning"}
                  className={`inline-flex items-center gap-1 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                    net.status === "provisioning"
                      ? "cursor-not-allowed text-gray-300"
                      : "text-gray-400 hover:bg-red-50 hover:text-red-600"
                  }`}
                  title="Delete network"
                >
                  <svg
                    className="h-4 w-4"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    strokeWidth={1.5}
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
                    />
                  </svg>
                  Delete
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
