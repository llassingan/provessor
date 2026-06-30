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
    terminated: "bg-gray-100 text-gray-500",
  };

  const labels: Record<string, string> = {
    pending: "Pending",
    provisioning: "Provisioning",
    running: "Running",
    stopped: "Stopped",
    failed: "Failed",
    terminated: "Terminated",
  };

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${styles[status] ?? "bg-gray-100 text-gray-700"}`}
    >
      {status === "provisioning" && (
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
      )}
      {labels[status] ?? status}
    </span>
  );
}

export default function VPSDetail(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const [instance, setInstance] = useState<VPS | null>(null);
  const [network, setNetwork] = useState<Network | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const navigate = useNavigate();

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
    if (events.length === 0) return;
    const last = events[events.length - 1];
    if (!last) return;
    if (last.status === "running" || last.status === "success" || last.status === "error" || last.status === "failed") {
      vps
        .get(numericId)
        .then(setInstance)
        .catch(() => {
          // silent refresh
        });
    }
  }, [events, numericId]);

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
    { label: "Public IP", value: instance.public_ip ?? "—", copyable: true },
    { label: "Private IP", value: instance.private_ip ?? "—", copyable: true },
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
