import { useState, type FormEvent, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { networks, settings, regions } from "../lib/api";
import type { Network, RegionGroup } from "../lib/api";
import { useSSE } from "../hooks/useSSE";
import ProvisioningLog from "../components/ProvisioningLog";

export default function NewNetwork(): JSX.Element {
  const [name, setName] = useState("");
  const [region, setRegion] = useState("");
  const [regionGroups, setRegionGroups] = useState<RegionGroup[]>([]);
  const [network, setNetwork] = useState<Network | null>(null);
  const [creating, setCreating] = useState(false);
  const [provisioning, setProvisioning] = useState(false);
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState("");
  const [showNoCredsGuard, setShowNoCredsGuard] = useState(false);
  const [checkingCredentials, setCheckingCredentials] = useState(true);

  const navigate = useNavigate();

  useEffect(() => {
    settings
      .get()
      .then((s) => {
        if (!s.tenancy_ocid) {
          setShowNoCredsGuard(true);
        }
      })
      .catch(() => {
        setShowNoCredsGuard(true);
      })
      .finally(() => {
        setCheckingCredentials(false);
      });
    regions.groups().then(setRegionGroups).catch(() => {});
  }, []);

  const { events, connected } = useSSE(
    network ? `/api/networks/${network.id}/events` : "",
  );

  useEffect(() => {
    if (events.length > 0) {
      const last = events[events.length - 1];
      if (last && (last.status === "ready" || last.status === "completed")) {
        setComplete(true);
      }
    }
  }, [events]);

  // Poll network status on SSE connect/reconnect — the "ready" event
  // may have been published while the SSE connection was dropped.
  useEffect(() => {
    if (!connected || !network || complete) return;
    networks.get(network.id).then((n) => {
      if (n.status === "ready") {
        setComplete(true);
      }
    }).catch(() => {});
  }, [connected, network, complete]);

  const handleCreate = useCallback(
    async (e: FormEvent): Promise<void> => {
      e.preventDefault();

      if (name.trim().length === 0) {
        setError("Please enter a network name.");
        return;
      }
      if (region === "") {
        setError("Please select a region.");
        return;
      }

      setCreating(true);
      setError("");
      try {
        const created = await networks.create(name.trim(), region);
        setNetwork(created);
      } catch (err: unknown) {
        setError(
          err instanceof Error ? err.message : "Failed to create network",
        );
      } finally {
        setCreating(false);
      }
    },
    [name, region],
  );

  const handleProvision = useCallback(async (): Promise<void> => {
    if (!network) return;

    setProvisioning(true);
    setError("");
    try {
      await networks.provision(network.id);
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to start provisioning",
      );
      setProvisioning(false);
    }
  }, [network]);

  return (
    <div className="mx-auto max-w-3xl">
      {checkingCredentials ? (
        <div className="flex h-64 items-center justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-200 border-t-primary-600" />
        </div>
      ) : showNoCredsGuard ? (
        <>
          <h1 className="mb-1 text-2xl font-bold text-gray-900">
            New Network
          </h1>
          <p className="mb-6 text-sm text-gray-500">
            Create a virtual cloud network with automatic CIDR allocation
          </p>

          <div className="rounded-xl border border-amber-200 bg-amber-50 p-6 text-center">
            <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-amber-100">
              <svg
                className="h-6 w-6 text-amber-600"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth={1.5}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
                />
              </svg>
            </div>
            <h3 className="mb-1 text-lg font-semibold text-amber-900">
              Credentials Required
            </h3>
            <p className="mb-4 text-sm text-amber-700">
              You haven't set up your OCI credentials yet. Configure your
              cloud account before creating a network.
            </p>
            <div className="flex justify-center gap-3">
              <button
                type="button"
                onClick={() => {
                  navigate("/settings");
                }}
                className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700"
              >
                Set up now
              </button>
              <button
                type="button"
                onClick={() => {
                  navigate("/networks");
                }}
                className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
              >
                I&apos;ll do it later
              </button>
            </div>
          </div>
        </>
      ) : (
        <>
      <h1 className="mb-1 text-2xl font-bold text-gray-900">
        New Network
      </h1>
      <p className="mb-6 text-sm text-gray-500">
        Create a virtual cloud network with automatic CIDR allocation
      </p>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {complete && network && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 p-6 text-center">
          <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-emerald-500">
            <svg
              className="h-6 w-6 text-white"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={3}
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M4.5 12.75l6 6 9-13.5"
              />
            </svg>
          </div>
          <h3 className="mb-1 text-lg font-semibold text-emerald-900">
            Network Ready
          </h3>
          <p className="mb-4 text-sm text-emerald-700">
            {network.name} has been provisioned successfully. The VCN and
            subnet are ready for VPS instances.
          </p>
          <button
            type="button"
            onClick={() => {
              navigate("/networks");
            }}
            className="rounded-lg bg-emerald-600 px-6 py-2 text-sm font-medium text-white hover:bg-emerald-700"
          >
            View Networks
          </button>
        </div>
      )}

      {!complete && network && (
        <div className="space-y-5">
          <div className="rounded-xl border border-gray-200 bg-white p-6">
            <h2 className="mb-4 text-lg font-semibold text-gray-900">
              Network Details
            </h2>
            <dl className="space-y-4">
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Name</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {network.name}
                </dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Region</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {network.region || "—"}
                </dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">VCN CIDR</dt>
                <dd className="font-mono text-sm font-medium text-gray-900">
                  {network.cidr_vcn || "Pending allocation"}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Subnet CIDR</dt>
                <dd className="font-mono text-sm font-medium text-gray-900">
                  {network.cidr_subnet || "Pending allocation"}
                </dd>
              </div>
            </dl>
          </div>

          {!provisioning && (
            <div className="flex justify-between">
              <button
                type="button"
                onClick={() => {
                  navigate("/networks");
                }}
                className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleProvision();
                }}
                className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700"
              >
                Provision Network
              </button>
            </div>
          )}

          {provisioning && (
            <div className="space-y-4">
              <h2 className="text-lg font-semibold text-gray-900">
                Provisioning Progress
              </h2>
              <ProvisioningLog events={events} connected={connected} />
            </div>
          )}
        </div>
      )}

      {!network && (
        <form
          onSubmit={(e) => {
            void handleCreate(e);
          }}
        >
          <div className="rounded-xl border border-gray-200 bg-white p-6 space-y-5">
            <div>
              <label
                htmlFor="network-name"
                className="mb-1 block text-sm font-medium text-gray-700"
              >
                Network Name
              </label>
              <input
                id="network-name"
                type="text"
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                }}
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
                placeholder="production-vcn"
                required
              />
            </div>

            <div>
              <label className="mb-2 block text-sm font-medium text-gray-700">Region</label>
              {regionGroups.length === 0 ? (
                <div className="text-sm text-gray-400">Loading regions...</div>
              ) : (
                <div className="space-y-4">
                  {regionGroups.map((group) => (
                    <div key={group.group}>
                      <div className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-gray-400">
                        {group.group}
                      </div>
                      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                        {group.items.map((item) => (
                          <button
                            key={item.key}
                            type="button"
                            onClick={() => setRegion(item.key)}
                            className={`rounded-lg border px-3 py-2 text-left text-sm transition-all ${
                              region === item.key
                                ? "border-primary-500 bg-primary-50 shadow-sm font-medium text-primary-700"
                                : "border-gray-200 bg-white hover:border-gray-300 text-gray-700"
                            }`}
                          >
                            {item.name}
                          </button>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="mt-6 flex justify-between">
            <button
              type="button"
              onClick={() => {
                navigate("/networks");
              }}
              className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={creating}
              className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {creating ? "Creating..." : "Create Network"}
            </button>
          </div>
        </form>
      )}
      </>
    )}
    </div>
  );
}
