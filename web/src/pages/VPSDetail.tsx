import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { vps, networks } from "../lib/api";
import type { VPS, Network } from "../lib/api";
import { useSSE } from "../hooks/useSSE";
import ProvisioningLog from "../components/ProvisioningLog";
import VPSActions from "../components/VPSActions";

function StatusBadge({ status }: { status: VPS["status"] }): JSX.Element {
  const styles: Record<string, string> = {
    pending: "bg-gray-100 text-gray-700",
    provisioning: "bg-blue-100 text-blue-700",
    running: "bg-emerald-100 text-emerald-700",
    stopped: "bg-amber-100 text-amber-700",
    failed: "bg-red-100 text-red-700",
    terminating: "bg-orange-100 text-orange-700",
    terminated: "bg-gray-100 text-gray-500",
  };

  const labels: Record<string, string> = {
    pending: "Pending",
    provisioning: "Provisioning",
    running: "Running",
    stopped: "Stopped",
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
      {status === "terminating" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-orange-500" />
      )}
      {labels[status] ?? status}
    </span>
  );
}

// ---------- Eye Icon ----------

function EyeIcon({ open }: { open: boolean }): JSX.Element {
  return (
    <svg
      className="h-4 w-4"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={1.5}
    >
      {open ? (
        <>
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z"
          />
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
          />
        </>
      ) : (
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88"
        />
      )}
    </svg>
  );
}

// ---------- Spinner ----------

function Spinner(): JSX.Element {
  return (
    <svg
      className="h-3.5 w-3.5 animate-spin"
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
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>
  );
}

export default function VPSDetail(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const [instance, setInstance] = useState<VPS | null>(null);
  const [network, setNetwork] = useState<Network | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refreshingIPs, setRefreshingIPs] = useState(false);
  const [showSshPassword, setShowSshPassword] = useState(false);
  const navigate = useNavigate();

  const handleRefreshIPs = async (): Promise<void> => {
    if (!instance) return;
    setRefreshingIPs(true);
    try {
      const updated = await vps.refreshIPs(instance.id);
      setInstance(updated);
    } catch {
      // silent — IPs may not have changed
    } finally {
      setRefreshingIPs(false);
    }
  };

  const numericId = id ? Number(id) : 0;

  const sseUrl = numericId > 0 ? `/api/vps/${numericId}/events` : "";
  const { events, connected } = useSSE(sseUrl);

  useEffect(() => {
    if (numericId === 0) return;

    setLoading(true);
    setError("");
    vps
      .get(numericId)
      .then((data) => {
        setInstance(data);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load VPS",
        );
      })
      .finally(() => {
        setLoading(false);
      });
  }, [numericId]);

  useEffect(() => {
    if (!instance?.network_id) return;
    networks.get(instance.network_id).then(setNetwork).catch(() => setNetwork(null));
  }, [instance?.network_id]);

  useEffect(() => {
    if (events.length === 0 || !instance) return;
    const last = events[events.length - 1];
    if (!last) return;
    if (last.status === "terminated" && instance.status !== "terminated") {
      setInstance({ ...instance, status: "terminated" });
      return;
    }
    if (last.status === "running" || last.status === "success" || last.status === "error" || last.status === "failed") {
      vps
        .get(numericId)
        .then(setInstance)
        .catch(() => {
          // silent refresh
        });
    }
  }, [events, numericId]);

  // Poll while terminating — OCI termination takes ~1min and the SSE
  // heartbeat keeps the connection alive, so the reconnect poll below
  // never triggers. Poll every 5s until status leaves "terminating".
  useEffect(() => {
    if (!instance || instance.status !== "terminating") return;
    const interval = setInterval(() => {
      vps.get(numericId).then((data) => {
        setInstance((prev) => {
          if (!prev || prev.status !== data.status) return data;
          return prev;
        });
      }).catch(() => {});
    }, 5000);
    return () => clearInterval(interval);
  }, [instance?.status, numericId]);

  // Poll VPS status on SSE connect/reconnect — events may have been missed
  // while the SSE connection was dropped during provisioning.
  useEffect(() => {
    if (!connected || !instance || instance.status === "running" || instance.status === "failed" || instance.status === "stopped" || instance.status === "terminated") return;
    vps.get(numericId).then((data) => {
      if (data.status !== instance.status) setInstance(data);
    }).catch(() => {});
  }, [connected, instance?.status, numericId]);

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl animate-pulse space-y-4">
        <div className="h-8 w-48 rounded bg-gray-200" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="h-24 rounded-xl bg-gray-100" />
          ))}
        </div>
      </div>
    );
  }

  if (error || !instance) {
    return (
      <div className="mx-auto max-w-4xl">
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error || "VPS not found"}
        </div>
        <button
          type="button"
          onClick={() => {
            navigate("/dashboard");
          }}
          className="mt-4 text-sm font-medium text-primary-600 hover:text-primary-500"
        >
          &larr; Back to Dashboard
        </button>
      </div>
    );
  }

  const infoCards = [
    { label: "Shape", value: instance.shape },
    { label: "OCPU", value: String(instance.ocpu) },
    { label: "Memory", value: `${instance.memory_gb} GB` },
    { label: "Boot Volume", value: `${instance.boot_volume_size_gb} GB` },
    { label: "Public IP", value: instance.public_ip ?? "—", copyable: true, refreshable: true },
    { label: "Private IP", value: instance.private_ip ?? "—", copyable: true, refreshable: true },
    network
      ? { label: "Network", value: `${network.name}${network.region ? " · " + network.region : ""}` }
      : { label: "Network", value: "—" },
  ];

  return (
    <div className="mx-auto max-w-4xl">
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <button
            type="button"
            onClick={() => {
              navigate("/dashboard");
            }}
            className="mb-2 text-sm font-medium text-primary-600 hover:text-primary-500"
          >
            &larr; Back to Dashboard
          </button>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold text-gray-900">
              {instance.display_name}
            </h1>
            <StatusBadge status={instance.status} />
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <VPSActions
            vpsInstance={instance}
            onUpdate={setInstance}
            onDelete={() => {
              navigate("/dashboard");
            }}
          />
          <button
            type="button"
            onClick={() => {
              navigate(`/vps/${instance.id}/manage`);
            }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50"
          >
            Manage
          </button>
        </div>
      </div>

      <div className="mb-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {infoCards.map((card) => (
          <div
            key={card.label}
            className="rounded-xl border border-gray-200 bg-white p-4"
          >
            <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">
              {card.label}
            </dt>
            <dd className="mt-1 flex items-center gap-1.5 text-sm font-medium text-gray-900">
              {card.copyable && card.value !== "—" ? (
                <>
                  <code className="rounded bg-gray-100 px-1.5 py-0.5 text-xs">
                    {card.value}
                  </code>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard.writeText(card.value);
                    }}
                    className="rounded p-0.5 text-gray-400 hover:text-gray-600"
                    title="Copy"
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
                  </button>
                  {"refreshable" in card && (
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        void handleRefreshIPs();
                      }}
                      disabled={refreshingIPs}
                      className="rounded p-0.5 text-gray-400 hover:text-gray-600 disabled:opacity-50"
                      title="Refresh IPs"
                    >
                      {refreshingIPs ? (
                        <Spinner />
                      ) : (
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
                            d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182"
                          />
                        </svg>
                      )}
                    </button>
                  )}
                </>
              ) : (
                <span>{card.value}</span>
              )}
            </dd>
          </div>
        ))}
      </div>

      {instance.initial_credentials && (
        <div className="mb-8 rounded-xl border border-gray-200 bg-white p-5">
          <div className="mb-3 flex items-center gap-2">
            <h2 className="text-sm font-semibold text-gray-900">
              Initial Credentials
            </h2>
            <button
              type="button"
              onClick={() => {
                void navigator.clipboard.writeText(
                  instance.initial_credentials ?? "",
                );
              }}
              className="rounded-lg border border-gray-200 px-2 py-1 text-xs font-medium text-gray-600 hover:bg-gray-50"
            >
              Copy
            </button>
          </div>
          <pre className="max-h-40 overflow-y-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-700">
            {instance.initial_credentials}
          </pre>
        </div>
      )}

      {instance.ssh_username && (
        <div className="mb-8 rounded-xl border border-gray-200 bg-white p-5">
          <div className="mb-3 flex items-center gap-2">
            <h2 className="text-sm font-semibold text-gray-900">
              SSH Credentials
            </h2>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-2">
              <div className="flex items-center gap-2">
                <span className="text-xs font-medium text-gray-500">Username</span>
                <code className="rounded bg-gray-200 px-1.5 py-0.5 text-xs text-gray-900">
                  {instance.ssh_username}
                </code>
              </div>
              <button
                type="button"
                onClick={() => {
                  void navigator.clipboard.writeText(instance.ssh_username ?? "");
                }}
                className="rounded p-1 text-gray-400 hover:text-gray-600"
                title="Copy username"
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
              </button>
            </div>
            {instance.ssh_password && (
              <div className="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-medium text-gray-500">Password</span>
                  <code className="rounded bg-gray-200 px-1.5 py-0.5 text-xs text-gray-900">
                    {showSshPassword ? instance.ssh_password : "••••••••"}
                  </code>
                </div>
                <div className="flex items-center gap-0.5">
                  <button
                    type="button"
                    onClick={() => {
                      setShowSshPassword((v) => !v);
                    }}
                    className="rounded p-1 text-gray-400 hover:text-gray-600"
                    title={showSshPassword ? "Hide password" : "Show password"}
                  >
                    <EyeIcon open={showSshPassword} />
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard.writeText(instance.ssh_password ?? "");
                    }}
                    className="rounded p-1 text-gray-400 hover:text-gray-600"
                    title="Copy password"
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
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      <div>
        <div className="mb-3 flex items-center gap-2">
          <h2 className="text-sm font-semibold text-gray-900">
            Provisioning Log
          </h2>
          <span
            className={`inline-block h-2 w-2 rounded-full ${connected ? "bg-emerald-500" : "bg-gray-300"}`}
          />
          <span className="text-xs text-gray-400">
            {connected ? "Live" : "Reconnecting"}
          </span>
        </div>
        <ProvisioningLog events={events} connected={connected} vpsStatus={instance.status} />
      </div>
    </div>
  );
}
