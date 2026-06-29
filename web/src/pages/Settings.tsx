import { useState, useEffect, type FormEvent } from "react";
import { settings } from "../lib/api";
import type { Settings, UpdateSettingsRequest } from "../lib/api";

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
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [showHelp, setShowHelp] = useState(false);

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

  return (
    <div className="mx-auto max-w-3xl space-y-8">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
        <button
          type="button"
          onClick={() => { setShowHelp(true); }}
          className="flex h-7 w-7 items-center justify-center rounded-full border border-gray-300 text-xs font-bold text-gray-500 transition-colors hover:border-gray-400 hover:text-gray-700"
          aria-label="Help"
        >
          ?
        </button>
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
              placeholder="-----BEGIN PRIVATE KEY-----\n..."
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

      {showHelp && (
        <HelpModal onClose={() => { setShowHelp(false); }} />
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

interface HelpModalProps {
  onClose: () => void;
}

function HelpModal({ onClose }: HelpModalProps): JSX.Element {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-lg rounded-xl bg-white p-6 shadow-xl">
        <h2 className="text-lg font-semibold text-gray-900">
          How to get OCI Credentials
        </h2>
        <p className="mt-1 text-sm text-gray-500">
          Follow these steps to obtain your Oracle Cloud Infrastructure credentials.
        </p>

        <div className="mt-6 space-y-4">
          <HelpStep
            number={1}
            label="Sign in to OCI Console"
            description="Go to cloud.oracle.com and sign in with your Oracle Cloud account"
          />
          <HelpStep
            number={2}
            label="Find Tenancy OCID"
            description="Click your profile (top right) → Click Tenancy (your-tenancy-name). Copy the Tenancy OCID from the details page"
          />
          <HelpStep
            number={3}
            label="Find User OCID"
            description="Click your profile (top right) → User setting → In details tab, copy OCID"
          />
          <HelpStep
            number={4}
            label="Get API Key Fingerprint, Private Key, and Public Key"
            description="Click your profile (top right) → User setting → Click tab Tokens and keys → click the Add API Key → Pick Generate API key pair option → Download Public and Private key pair → Click Add button at the bottom → copy the fingerprint and region"
          />
          <HelpStep
            number={5}
            label="Compartment OCID"
            description="Click your profile (top right) → User setting → on the left pane select Compartments → Click Create Compartment button → enter Compartment name, description, and parents (use your default compartment) → Copy the compartment OCID"
          />
          <HelpStep
            number={6}
            label="API Base URL"
            description="API base URL is the appropriate OCI API endpoint for your region. Use this format as your base URL: https://iaas.&lt;region&gt;.oraclecloud.com"
          />
        </div>

        <div className="mt-6">
          <button
            type="button"
            onClick={onClose}
            className="w-full rounded-lg bg-primary-600 px-4 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-primary-700"
          >
            Got it
          </button>
        </div>
      </div>
    </div>
  );
}

function HelpStep({
  number,
  label,
  description,
}: {
  number: number;
  label: string;
  description: string;
}): JSX.Element {
  return (
    <div className="flex gap-3">
      <div className="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700">
        {number}
      </div>
      <div>
        <p className="text-sm font-medium text-gray-900">{label}</p>
        <p className="text-xs text-gray-500">{description}</p>
      </div>
    </div>
  );
}
