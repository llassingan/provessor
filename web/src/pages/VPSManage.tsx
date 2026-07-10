import { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { vps } from "../lib/api";
import type { VPS, FirewallRule, FirewallRules } from "../lib/api";

type Tab = "firewall" | "password" | "restart" | "reset";

const TAB_LABELS: { key: Tab; label: string }[] = [
  { key: "firewall", label: "Firewall" },
  { key: "password", label: "Reset Password" },
  { key: "restart", label: "Restart Instance" },
  { key: "reset", label: "Reset Instance" },
];

const tabStyles: Record<Tab, { base: string; active: string }> = {
  firewall: {
    base: "text-gray-500 hover:text-blue-600 border-transparent hover:border-blue-300",
    active: "text-blue-700 border-blue-600 font-medium",
  },
  password: {
    base: "text-gray-500 hover:text-amber-600 border-transparent hover:border-amber-300",
    active: "text-amber-700 border-amber-600 font-medium",
  },
  restart: {
    base: "text-gray-500 hover:text-emerald-600 border-transparent hover:border-emerald-300",
    active: "text-emerald-700 border-emerald-600 font-medium",
  },
  reset: {
    base: "text-gray-500 hover:text-red-600 border-transparent hover:border-red-300",
    active: "text-red-700 border-red-600 font-medium",
  },
};

// ---------- StatusBadge (same as VPSDetail) ----------

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

// ---------- CIDR validation ----------

function isValidCIDR(value: string): boolean {
  if (value === "") return true; // optional field
  const cidrRegex =
    /^((25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\/([0-9]|[12]\d|3[0-2])$/;
  return cidrRegex.test(value);
}

// ---------- Spinner ----------

function Spinner(): JSX.Element {
  return (
    <svg
      className="h-4 w-4 animate-spin"
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

// ---------- Main Component ----------

export default function VPSManage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const numericId = id !== undefined ? Number(id) : 0;

  const [instance, setInstance] = useState<VPS | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refreshingIPs, setRefreshingIPs] = useState(false);
  const [activeTab, setActiveTab] = useState<Tab>("firewall");

  useEffect(() => {
    if (numericId === 0) return;
    setLoading(true);
    setError("");
    vps
      .get(numericId)
      .then(setInstance)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Failed to load VPS");
      })
      .finally(() => {
        setLoading(false);
      });
  }, [numericId]);

  const refreshVPS = (): void => {
    if (numericId === 0) return;
    vps.get(numericId).then(setInstance).catch(() => {
      // silent refresh
    });
  };

  const handleRefreshIPs = async (): Promise<void> => {
    if (!instance) return;
    setRefreshingIPs(true);
    try {
      const updated = await vps.refreshIPs(instance.id);
      setInstance(updated);
    } catch {
      // silent
    } finally {
      setRefreshingIPs(false);
    }
  };

  // ---------- Loading / Error / Not Found ----------

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl animate-pulse space-y-4">
        <div className="h-8 w-48 rounded bg-gray-200" />
        <div className="h-6 w-64 rounded bg-gray-200" />
        <div className="grid gap-4 sm:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <div key={i} className="h-16 rounded-xl bg-gray-100" />
          ))}
        </div>
        <div className="h-12 rounded bg-gray-200" />
        <div className="h-48 rounded-xl bg-gray-100" />
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

  return (
    <div className="mx-auto max-w-4xl">
      {/* Header */}
      <div className="mb-6">
        <button
          type="button"
          onClick={() => {
            navigate(`/vps/${String(instance.id)}`);
          }}
          className="mb-2 text-sm font-medium text-primary-600 hover:text-primary-500"
        >
          &larr; Back to VPS
        </button>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-bold text-gray-900">
            {instance.display_name}
          </h1>
          <StatusBadge status={instance.status} />
        </div>
      </div>

      {/* Quick Info Row */}
      <div className="mb-6 grid gap-4 sm:grid-cols-4">
        {[
          { label: "Public IP", value: instance.public_ip ?? "—", refreshable: true },
          { label: "Shape", value: instance.shape },
          { label: "OCPU", value: String(instance.ocpu) },
          { label: "Memory", value: `${String(instance.memory_gb)} GB` },
        ].map((info) => (
          <div
            key={info.label}
            className="rounded-xl border border-gray-200 bg-white p-3"
          >
            <dt className="text-xs font-medium uppercase tracking-wide text-gray-400">
              {info.label}
            </dt>
            <dd className="mt-0.5 flex items-center gap-1.5 text-sm font-medium text-gray-900">
              <span>{info.value}</span>
              {"refreshable" in info && (
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
            </dd>
          </div>
        ))}
      </div>

      {/* Tab Bar */}
      <div className="mb-6 border-b border-gray-200">
        <nav className="-mb-px flex gap-6" aria-label="Tabs">
          {TAB_LABELS.map((tab) => {
            const active = activeTab === tab.key;
            const style = tabStyles[tab.key];
            return (
              <button
                key={tab.key}
                type="button"
                onClick={() => {
                  setActiveTab(tab.key);
                }}
                className={`whitespace-nowrap border-b-2 px-1 pb-3 text-sm transition-colors ${
                  active ? style.active : style.base
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>
      </div>

      {/* Tab Content */}
      {activeTab === "firewall" && (
        <FirewallTab vpsId={instance.id} />
      )}
      {activeTab === "password" && (
        <PasswordTab vpsId={instance.id} />
      )}
      {activeTab === "restart" && (
        <RestartTab vpsId={instance.id} onSuccess={refreshVPS} />
      )}
      {activeTab === "reset" && (
        <ResetTab vpsId={instance.id} onSuccess={() => {
          navigate(`/vps/${String(instance.id)}`);
        }} />
      )}
    </div>
  );
}

// ==================== FIREWALL TAB ====================

function FirewallTab({ vpsId }: { vpsId: number }): JSX.Element {
  const [rules, setRules] = useState<FirewallRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [showForm, setShowForm] = useState(false);

  // Form state
  const [newPort, setNewPort] = useState("");
  const [newName, setNewName] = useState("");
  const [newDesc, setNewDesc] = useState("");
  const [newDirection, setNewDirection] = useState<"ingress" | "egress">(
    "ingress",
  );
  const [newSource, setNewSource] = useState("");
  const [newDest, setNewDest] = useState("");
  const [formError, setFormError] = useState("");

  const fetchRules = (): void => {
    setLoading(true);
    setError("");
    vps
      .getFirewall(vpsId)
      .then((data: FirewallRules) => {
        setRules([...data.ingress, ...data.egress]);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load firewall rules",
        );
      })
      .finally(() => {
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchRules();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vpsId]);

  const resetForm = (): void => {
    setNewPort("");
    setNewName("");
    setNewDesc("");
    setNewDirection("ingress");
    setNewSource("");
    setNewDest("");
    setFormError("");
    setShowForm(false);
  };

  const validateForm = (): boolean => {
    const portNum = Number(newPort);
    if (Number.isNaN(portNum) || portNum < 1 || portNum > 65535) {
      setFormError("Port must be a number between 1 and 65535.");
      return false;
    }
    if (newName.trim() === "") {
      setFormError("Rule name is required.");
      return false;
    }
    if (newDirection === "ingress" && newSource !== "" && !isValidCIDR(newSource)) {
      setFormError("Source must be a valid CIDR (e.g. 0.0.0.0/0).");
      return false;
    }
    if (newDirection === "egress" && newDest !== "" && !isValidCIDR(newDest)) {
      setFormError("Destination must be a valid CIDR (e.g. 0.0.0.0/0).");
      return false;
    }
    return true;
  };

  const handleAddRule = (): void => {
    if (!validateForm()) return;

    const newRule: FirewallRule = {
      port: Number(newPort),
      name: newName.trim(),
      description: newDesc.trim(),
      direction: newDirection,
    };

    if (newDirection === "ingress" && newSource.trim()) {
      newRule.source = newSource.trim();
    }
    if (newDirection === "egress" && newDest.trim()) {
      newRule.destination = newDest.trim();
    }

    setRules((prev) => [...prev, newRule]);
    resetForm();
  };

  const handleDeleteRule = (index: number): void => {
    setRules((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSave = async (): Promise<void> => {
    setSaving(true);
    setMessage("");
    setError("");
    try {
      const result = await vps.updateFirewall(vpsId, rules);
      const combinedRules = [...result.ingress, ...result.egress];
      setRules(combinedRules);
      setMessage("Firewall rules saved successfully.");
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to save firewall rules",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      {(error || message) && (
        <div
          className={`mb-4 rounded-lg border px-4 py-3 text-sm ${
            error
              ? "border-red-200 bg-red-50 text-red-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700"
          }`}
        >
          {error || message}
        </div>
      )}

      {loading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }, (_, i) => (
            <div
              key={i}
              className="h-12 animate-pulse rounded-lg bg-gray-100"
            />
          ))}
        </div>
      ) : rules.length === 0 ? (
        <div className="rounded-xl border border-dashed border-gray-300 bg-white p-8 text-center">
          <p className="text-sm text-gray-500">No firewall rules configured</p>
        </div>
      ) : (
        <div className="mb-4 overflow-hidden rounded-xl border border-gray-200">
          <table className="w-full text-left text-sm">
            <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wide text-gray-400">
              <tr>
                <th className="px-4 py-3">Port</th>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Description</th>
                <th className="px-4 py-3">Direction</th>
                <th className="px-4 py-3">Source / Dest</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {rules.map((rule, i) => (
                <tr key={i} className="bg-white hover:bg-gray-50/50">
                  <td className="px-4 py-3 font-mono text-xs text-gray-900">
                    {rule.port}
                  </td>
                  <td className="px-4 py-3 text-gray-700">{rule.name}</td>
                  <td className="px-4 py-3 text-gray-500">
                    {rule.description || "—"}
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
                        rule.direction === "ingress"
                          ? "bg-blue-100 text-blue-700"
                          : "bg-purple-100 text-purple-700"
                      }`}
                    >
                      {rule.direction}
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-gray-500">
                    {rule.source ?? rule.destination ?? "—"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      type="button"
                      onClick={() => {
                        handleDeleteRule(i);
                      }}
                      className="rounded p-1 text-gray-400 hover:bg-red-50 hover:text-red-600 transition-colors"
                      title="Delete rule"
                    >
                      <svg
                        className="h-4 w-4"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth={2}
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          d="M6 18L18 6M6 6l12 12"
                        />
                      </svg>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Add Rule Form */}
      {showForm ? (
        <div className="mb-4 rounded-xl border border-blue-200 bg-blue-50/50 p-4">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-blue-800">
              Add Firewall Rule
            </h3>
            <button
              type="button"
              onClick={resetForm}
              className="rounded p-1 text-gray-400 hover:text-gray-600"
            >
              <svg
                className="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth={2}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            </button>
          </div>

          {formError && (
            <div className="mb-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
              {formError}
            </div>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs font-medium text-gray-600">
                Port
              </label>
              <input
                type="number"
                min={1}
                max={65535}
                value={newPort}
                onChange={(e) => {
                  setNewPort(e.target.value);
                }}
                placeholder="e.g. 443"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-gray-600">
                Name
              </label>
              <input
                type="text"
                value={newName}
                onChange={(e) => {
                  setNewName(e.target.value);
                }}
                placeholder="e.g. HTTPS"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div className="sm:col-span-2">
              <label className="mb-1 block text-xs font-medium text-gray-600">
                Description
              </label>
              <input
                type="text"
                value={newDesc}
                onChange={(e) => {
                  setNewDesc(e.target.value);
                }}
                placeholder="Optional description"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-gray-600">
                Direction
              </label>
              <select
                value={newDirection}
                onChange={(e) => {
                  setNewDirection(e.target.value as "ingress" | "egress");
                }}
                className="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              >
                <option value="ingress">Ingress</option>
                <option value="egress">Egress</option>
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-gray-600">
                {newDirection === "ingress" ? "Source CIDR" : "Destination CIDR"}
              </label>
              <input
                type="text"
                value={newDirection === "ingress" ? newSource : newDest}
                onChange={(e) => {
                  if (newDirection === "ingress") {
                    setNewSource(e.target.value);
                  } else {
                    setNewDest(e.target.value);
                  }
                }}
                placeholder="e.g. 0.0.0.0/0"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
          </div>

          <div className="mt-4 flex gap-3">
            <button
              type="button"
              onClick={handleAddRule}
              className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition-colors"
            >
              Add Rule
            </button>
            <button
              type="button"
              onClick={resetForm}
              className="rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors"
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => {
            setShowForm(true);
          }}
          className="inline-flex items-center gap-1.5 rounded-lg border border-blue-200 bg-white px-4 py-2 text-sm font-medium text-blue-600 transition-colors hover:bg-blue-50"
        >
          <svg
            className="h-4 w-4"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth={2}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M12 4.5v15m7.5-7.5h-15"
            />
          </svg>
          Add Rule
        </button>
      )}

      {/* Save Button */}
      <div className="mt-6 border-t border-gray-200 pt-4">
        <button
          type="button"
          onClick={() => {
            void handleSave();
          }}
          disabled={saving}
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700 disabled:opacity-50"
        >
          {saving && <Spinner />}
          {saving ? "Saving..." : "Save Changes"}
        </button>
      </div>
    </div>
  );
}

// ==================== PASSWORD TAB ====================

function PasswordTab({ vpsId }: { vpsId: number }): JSX.Element {
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);
  const [updatedPassword, setUpdatedPassword] = useState("");
  const [showUpdatedPassword, setShowUpdatedPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const validatePassword = (): boolean => {
    if (password.trim() === "") {
      setError("Password cannot be empty or whitespace only.");
      return false;
    }
    if (/[\n\r\0]/.test(password)) {
      setError("Password cannot contain new lines or NUL characters.");
      return false;
    }
    if (password.length < 12) {
      setError("Password must be at least 12 characters.");
      return false;
    }
    if (password.length > 128) {
      setError("Password must be 128 characters or fewer.");
      return false;
    }
    if (password !== confirm) {
      setError("Passwords do not match.");
      return false;
    }
    return true;
  };

  const handleSubmit = async (): Promise<void> => {
    setMessage("");
    setError("");
    setUpdatedPassword("");
    setShowUpdatedPassword(false);

    if (!validatePassword()) return;

    setLoading(true);
    try {
      const updated = await vps.resetPassword(vpsId, password);
      if (updated.ssh_password === undefined || updated.ssh_password === "") {
        setError(
          "Password reset response did not include the updated SSH password.",
        );
        return;
      }
      setUpdatedPassword(updated.ssh_password);
      setMessage("Password reset successfully.");
      setPassword("");
      setConfirm("");
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to reset password",
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {(error || message) && (
        <div
          className={`mb-4 rounded-lg border px-4 py-3 text-sm ${
            error
              ? "border-red-200 bg-red-50 text-red-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700"
          }`}
        >
          {error || message}
        </div>
      )}

      <div className="max-w-md rounded-xl border border-amber-200 bg-amber-50/50 p-6">
        <h3 className="mb-1 text-sm font-semibold text-amber-800">
          Reset Instance Password
        </h3>
        <p className="mb-4 text-xs text-amber-600">
          This will set a new password for the provisioned SSH user. The old
          password will no longer work.
        </p>

        {updatedPassword !== "" && (
          <div className="mb-4 rounded-lg border border-emerald-200 bg-white p-3">
            <div className="mb-2 flex items-center justify-between gap-3">
              <span className="text-xs font-semibold text-emerald-800">
                Updated SSH Password
              </span>
              <span className="text-xs text-emerald-700">
                Store this securely. It is shown because you just reset it.
              </span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-2">
              <code className="break-all rounded bg-gray-200 px-1.5 py-0.5 text-xs text-gray-900">
                {showUpdatedPassword ? updatedPassword : "••••••••"}
              </code>
              <div className="ml-3 flex shrink-0 items-center gap-0.5">
                <button
                  type="button"
                  onClick={() => {
                    setShowUpdatedPassword((v) => !v);
                  }}
                  className="rounded p-1 text-gray-400 hover:text-gray-600"
                  title={showUpdatedPassword ? "Hide password" : "Show password"}
                >
                  <EyeIcon open={showUpdatedPassword} />
                </button>
                <button
                  type="button"
                  onClick={() => {
                    void navigator.clipboard.writeText(updatedPassword);
                  }}
                  className="rounded p-1 text-gray-400 hover:text-gray-600"
                  title="Copy updated SSH password"
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
          </div>
        )}

        <div className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-gray-600">
              New Password
            </label>
            <div className="relative">
              <input
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                }}
                placeholder="12 to 128 characters"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 pr-10 text-sm focus:border-amber-500 focus:outline-none focus:ring-1 focus:ring-amber-500"
              />
              <button
                type="button"
                onClick={() => {
                  setShowPassword((v) => !v);
                }}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                title={showPassword ? "Hide password" : "Show password"}
              >
                <EyeIcon open={showPassword} />
              </button>
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs font-medium text-gray-600">
              Confirm Password
            </label>
            <div className="relative">
              <input
                type={showConfirm ? "text" : "password"}
                value={confirm}
                onChange={(e) => {
                  setConfirm(e.target.value);
                }}
                placeholder="Re-enter password"
                className="w-full rounded-lg border border-gray-300 px-3 py-2 pr-10 text-sm focus:border-amber-500 focus:outline-none focus:ring-1 focus:ring-amber-500"
              />
              <button
                type="button"
                onClick={() => {
                  setShowConfirm((v) => !v);
                }}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                title={showConfirm ? "Hide password" : "Show password"}
              >
                <EyeIcon open={showConfirm} />
              </button>
            </div>
          </div>

          <button
            type="button"
            onClick={() => {
              void handleSubmit();
            }}
            disabled={loading}
            className="inline-flex items-center gap-2 rounded-lg bg-amber-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-amber-700 disabled:opacity-50"
          >
            {loading && <Spinner />}
            {loading ? "Resetting..." : "Reset Password"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==================== RESTART TAB ====================

function RestartTab({
  vpsId,
  onSuccess,
}: {
  vpsId: number;
  onSuccess: () => void;
}): JSX.Element {
  const [confirming, setConfirming] = useState(false);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const handleRestart = async (): Promise<void> => {
    setLoading(true);
    setMessage("");
    setError("");
    setConfirming(false);
    try {
      await vps.restart(vpsId);
      setMessage("Instance is restarting. This may take a minute.");
      onSuccess();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to restart instance",
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {(error || message) && (
        <div
          className={`mb-4 rounded-lg border px-4 py-3 text-sm ${
            error
              ? "border-red-200 bg-red-50 text-red-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700"
          }`}
        >
          {error || message}
        </div>
      )}

      <div className="max-w-md rounded-xl border border-emerald-200 bg-emerald-50/50 p-6">
        <h3 className="mb-1 text-sm font-semibold text-emerald-800">
          Restart Instance
        </h3>
        <p className="mb-4 text-xs text-emerald-600">
          Performs a graceful operating system reboot. The instance will be
          temporarily unavailable during the restart. Running applications will
          be stopped and restarted cleanly. No data is lost.
        </p>

        {!confirming ? (
          <button
            type="button"
            onClick={() => {
              setConfirming(true);
            }}
            className="inline-flex items-center gap-1.5 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-emerald-700"
          >
            Restart Instance
          </button>
        ) : (
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => {
                void handleRestart();
              }}
              disabled={loading}
              className="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-emerald-700 disabled:opacity-50"
            >
              {loading && <Spinner />}
              {loading ? "Restarting..." : "Confirm Restart"}
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirming(false);
              }}
              className="rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors"
            >
              Cancel
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// ==================== RESET TAB ====================

function ResetTab({
  vpsId,
  onSuccess,
}: {
  vpsId: number;
  onSuccess: () => void;
}): JSX.Element {
  const [typedConfirm, setTypedConfirm] = useState("");
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const handleReset = async (): Promise<void> => {
    if (typedConfirm !== "RESET") return;

    setLoading(true);
    setMessage("");
    setError("");
    try {
      await vps.reset(vpsId);
      setMessage("Instance has been reset successfully.");
      onSuccess();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to reset instance",
      );
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {(error || message) && (
        <div
          className={`mb-4 rounded-lg border px-4 py-3 text-sm ${
            error
              ? "border-red-200 bg-red-50 text-red-700"
              : "border-emerald-200 bg-emerald-50 text-emerald-700"
          }`}
        >
          {error || message}
        </div>
      )}

      <div className="max-w-lg rounded-xl border-2 border-red-200 bg-red-50/30 p-6">
        <div className="mb-4 flex items-start gap-3">
          <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-red-100">
            <svg
              className="h-5 w-5 text-red-600"
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
          <div>
            <h3 className="text-sm font-semibold text-red-800">
              Reset Instance
            </h3>
            <p className="mt-1 text-xs text-red-700">
              This will <strong>permanently destroy</strong> the current
              instance and create a replacement. You will receive a{" "}
              <strong>new public IP address</strong> and{" "}
              <strong>new root credentials</strong>. All data on the current
              instance will be lost. This action cannot be undone.
            </p>
          </div>
        </div>

        <div className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-gray-700">
              Type <code className="rounded bg-red-100 px-1.5 py-0.5 text-red-700">RESET</code> to confirm
            </label>
            <input
              type="text"
              value={typedConfirm}
              onChange={(e) => {
                setTypedConfirm(e.target.value);
              }}
              placeholder="RESET"
              className="w-full rounded-lg border border-red-300 bg-white px-3 py-2 text-sm focus:border-red-500 focus:outline-none focus:ring-1 focus:ring-red-500"
            />
          </div>

          <button
            type="button"
            onClick={() => {
              void handleReset();
            }}
            disabled={typedConfirm !== "RESET" || loading}
            className="inline-flex items-center gap-2 rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {loading && <Spinner />}
            {loading ? "Resetting..." : "Reset Instance"}
          </button>
        </div>
      </div>
    </div>
  );
}
