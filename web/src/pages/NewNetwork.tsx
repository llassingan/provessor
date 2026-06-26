import { useState, type FormEvent, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { networks } from "../lib/api";
import type { Network } from "../lib/api";
import { useSSE } from "../hooks/useSSE";
import ProvisioningLog from "../components/ProvisioningLog";

export default function NewNetwork(): JSX.Element {
  const [name, setName] = useState("");
  const [network, setNetwork] = useState<Network | null>(null);
  const [creating, setCreating] = useState(false);
  const [provisioning, setProvisioning] = useState(false);
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState("");

  const navigate = useNavigate();

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

  const handleCreate = useCallback(
    async (e: FormEvent): Promise<void> => {
      e.preventDefault();

      if (name.trim().length === 0) {
        setError("Please enter a network name.");
        return;
      }

      setCreating(true);
      setError("");
      try {
        const created = await networks.create(name.trim());
        setNetwork(created);
      } catch (err: unknown) {
        setError(
          err instanceof Error ? err.message : "Failed to create network",
        );
      } finally {
        setCreating(false);
      }
    },
    [name],
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
          <div className="rounded-xl border border-gray-200 bg-white p-6">
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
    </div>
  );
}
