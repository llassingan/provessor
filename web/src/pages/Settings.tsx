import { useState, useEffect, type FormEvent } from "react";
import { settings } from "../lib/api";
import type { Settings, UpdateSettingsRequest } from "../lib/api";
import { useSSE } from "../hooks/useSSE";
import ProvisioningLog from "../components/ProvisioningLog";

interface SettingsPageProps {
  settings: Settings | null;
  onSettingsRefresh: () => void;
}

interface CredentialsForm {
  tenancy_ocid: string;
  user_ocid: string;
  fingerprint: string;
  private_key: string;
  region: string;
  compartment_ocid: string;
  api_base_url: string;
  api_token: string;
}

const EMPTY_FORM: CredentialsForm = {
  tenancy_ocid: "",
  user_ocid: "",
  fingerprint: "",
  private_key: "",
  region: "",
  compartment_ocid: "",
  api_base_url: "",
  api_token: "",
};

function formFromSettings(s: Settings | null): CredentialsForm {
  if (!s) return { ...EMPTY_FORM };
  return {
    tenancy_ocid: s.tenancy_ocid,
    user_ocid: s.user_ocid,
    fingerprint: s.fingerprint,
    private_key: s.private_key,
    region: s.region,
    compartment_ocid: s.compartment_ocid,
    api_base_url: s.api_base_url,
    api_token: "",
  };
}

export default function SettingsPage({
  settings: appSettings,
  onSettingsRefresh,
}: SettingsPageProps): JSX.Element {
  const [form, setForm] = useState<CredentialsForm>(() =>
    formFromSettings(appSettings),
  );
  const [saving, setSaving] = useState(false);
  const [provisioning, setProvisioning] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const { events, connected } = useSSE("/api/network/events");

  useEffect(() => {
    setForm(formFromSettings(appSettings));
  }, [appSettings]);

  const handleSave = async (e: FormEvent): Promise<void> => {
    e.preventDefault();
    setError("");
    setSuccess("");

    const missing = Object.entries(form).filter(
      ([, v]) => v.trim().length === 0,
    );
    if (missing.length > 0) {
      setError(`Missing fields: ${missing.map(([k]) => k).join(", ")}`);
      return;
    }

    if (
      form.private_key !== "" &&
      form.private_key !== "********" &&
      !form.private_key.includes("-----BEGIN PRIVATE KEY-----")
    ) {
      setError("Private key must contain -----BEGIN PRIVATE KEY-----");
      return;
    }

    const req: UpdateSettingsRequest = {
      tenancy_ocid: form.tenancy_ocid.trim(),
      user_ocid: form.user_ocid.trim(),
      fingerprint: form.fingerprint.trim(),
      private_key: form.private_key.trim(),
      region: form.region.trim(),
      compartment_ocid: form.compartment_ocid.trim(),
      api_base_url: form.api_base_url.trim(),
      api_token: form.api_token.trim(),
    };

    setSaving(true);
    try {
      await settings.update(req);
      setSuccess("Credentials saved successfully.");
      onSettingsRefresh();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to save settings",
      );
    } finally {
      setSaving(false);
    }
  };

  const handleProvision = async (): Promise<void> => {
    setError("");
    setProvisioning(true);
    try {
      await settings.provisionNetwork();
      onSettingsRefresh();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Network provisioning failed",
      );
    } finally {
      setProvisioning(false);
    }
  };

  const hasCredentials = appSettings !== null && appSettings.tenancy_ocid !== "";
  const networkReady = appSettings?.network_provisioned ?? false;

  return (
    <div className="mx-auto max-w-3xl space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
        <p className="mt-1 text-sm text-gray-500">
          Configure your Oracle Cloud Infrastructure connection
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {success && (
        <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
          {success}
        </div>
      )}

      <div className="rounded-xl border border-gray-200 bg-white p-6">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          OCI Credentials
        </h2>

        <form onSubmit={handleSave} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField
              label="Tenancy OCID"
              value={form.tenancy_ocid}
              onChange={(v) => {
                setForm((f) => ({ ...f, tenancy_ocid: v }));
              }}
            />
            <TextField
              label="User OCID"
              value={form.user_ocid}
              onChange={(v) => {
                setForm((f) => ({ ...f, user_ocid: v }));
              }}
            />
            <TextField
              label="Fingerprint"
              value={form.fingerprint}
              onChange={(v) => {
                setForm((f) => ({ ...f, fingerprint: v }));
              }}
            />
            <TextField
              label="Region"
              value={form.region}
              onChange={(v) => {
                setForm((f) => ({ ...f, region: v }));
              }}
            />
            <TextField
              label="Compartment OCID"
              value={form.compartment_ocid}
              onChange={(v) => {
                setForm((f) => ({ ...f, compartment_ocid: v }));
              }}
            />
            <TextField
              label="API Base URL"
              value={form.api_base_url}
              onChange={(v) => {
                setForm((f) => ({ ...f, api_base_url: v }));
              }}
            />
            <TextField
              label="API Token"
              value={form.api_token}
              onChange={(v) => {
                setForm((f) => ({ ...f, api_token: v }));
              }}
            />
          </div>

          <div>
            <label className="mb-1 block text-sm font-medium text-gray-700">
              Private Key
            </label>
            <textarea
              rows={6}
              value={form.private_key}
              onChange={(e) => {
                setForm((f) => ({ ...f, private_key: e.target.value }));
              }}
              className="w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
              placeholder="-----BEGIN PRIVATE KEY-----&#10;..."
            />
          </div>

          <button
            type="submit"
            disabled={saving}
            className="rounded-lg bg-primary-600 px-6 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {saving ? "Saving..." : "Save Credentials"}
          </button>
        </form>
      </div>

      <div className="rounded-xl border border-gray-200 bg-white p-6">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          Network Setup
        </h2>

        {appSettings && (appSettings.vcn_ocid || appSettings.subnet_ocid) && (
          <div className="mb-4 grid gap-3 sm:grid-cols-2">
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
              <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">
                VCN OCID
              </dt>
              <dd className="mt-1 break-all font-mono text-xs text-gray-700">
                {appSettings.vcn_ocid || "—"}
              </dd>
            </div>
            <div className="rounded-lg border border-gray-200 bg-gray-50 p-3">
              <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">
                Subnet OCID
              </dt>
              <dd className="mt-1 break-all font-mono text-xs text-gray-700">
                {appSettings.subnet_ocid || "—"}
              </dd>
            </div>
          </div>
        )}

        <div className="mb-4 flex items-center gap-3">
          <span className="text-sm text-gray-600">Status:</span>
          <span
            className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ${
              networkReady
                ? "bg-emerald-100 text-emerald-700"
                : "bg-gray-100 text-gray-600"
            }`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${networkReady ? "bg-emerald-500" : "bg-gray-400"}`}
            />
            {networkReady ? "Provisioned" : "Not provisioned"}
          </span>
        </div>

        {!networkReady && (
          <button
            type="button"
            onClick={() => {
              void handleProvision();
            }}
            disabled={!hasCredentials || provisioning}
            className="rounded-lg bg-primary-600 px-6 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {provisioning ? "Provisioning..." : "Setup Network"}
          </button>
        )}

        {!hasCredentials && (
          <p className="mt-2 text-xs text-gray-400">
            Save your OCI credentials first to enable network setup.
          </p>
        )}
      </div>

      {events.length > 0 && (
        <div>
          <div className="mb-3 flex items-center gap-2">
            <h3 className="text-sm font-semibold text-gray-900">
              Provisioning Progress
            </h3>
            <span
              className={`inline-block h-2 w-2 rounded-full ${connected ? "bg-emerald-500" : "bg-gray-300"}`}
            />
          </div>
          <ProvisioningLog events={events} connected={connected} />
        </div>
      )}
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}): JSX.Element {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium text-gray-700">
        {label}
      </label>
      <input
        type="text"
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
        className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
      />
    </div>
  );
}
