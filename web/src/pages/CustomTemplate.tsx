import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { templates } from "../lib/api";

const AVAILABLE_SHAPES = [
	"VM.Standard.E4.Flex",
	"VM.Standard.E5.Flex",
	"VM.Standard.A1.Flex",
];

export default function CustomTemplate(): JSX.Element {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [shape, setShape] = useState(AVAILABLE_SHAPES[0] ?? "VM.Standard.E4.Flex");
  const [ocpu, setOcpu] = useState(2);
  const [memory, setMemory] = useState(4);
  const [bootVolume, setBootVolume] = useState(50);
  const [cloudInitYaml, setCloudInitYaml] = useState("");
  const [logoUrl, setLogoUrl] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();

  const handleSubmit = async (e: FormEvent): Promise<void> => {
    e.preventDefault();
    setError("");
    setSuccess("");

    if (name.trim().length === 0) {
      setError("Template name is required.");
      return;
    }
    if (shape.trim().length === 0) {
      setError("Shape is required.");
      return;
    }
    if (cloudInitYaml.trim().length === 0) {
      setError("Cloud-init YAML is required.");
      return;
    }
    if (ocpu < 1 || ocpu > 64) {
      setError("OCPU must be between 1 and 64.");
      return;
    }
    if (memory < 1 || memory > 1024) {
      setError("Memory must be between 1 and 1024 GB.");
      return;
    }
		if (bootVolume < 50 || bootVolume > 200) {
			setError("Boot volume must be between 50 and 200 GB.");
      return;
    }

    setSubmitting(true);
    try {
      await templates.create({
        name: name.trim(),
        description: description.trim(),
        shape,
        default_ocpu: ocpu,
        default_memory: memory,
        boot_volume_size_gb: bootVolume,
        cloud_init_yaml: cloudInitYaml.trim(),
        ...(logoUrl.trim() ? { logo_url: logoUrl.trim() } : {}),
      });
      setSuccess("Template created successfully!");
      setName("");
      setDescription("");
      setCloudInitYaml("");
      setLogoUrl("");
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to create template",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-2xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">
            Custom Template
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Create a reusable deployment template with cloud-init automation
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            navigate("/vps/new");
          }}
          className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
        >
          Back to New VPS
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
          {success}{" "}
          <button
            type="button"
            onClick={() => {
              navigate("/vps/new");
            }}
            className="font-medium underline underline-offset-2"
          >
            Go to New VPS
          </button>
        </div>
      )}

      <form
        onSubmit={handleSubmit}
        className="space-y-6 rounded-xl border border-gray-200 bg-white p-6"
      >
        <div>
          <label
            htmlFor="tpl-name"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Template Name
          </label>
          <input
            id="tpl-name"
            type="text"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
            placeholder="e.g. LAMP Stack"
            required
          />
        </div>

        <div>
          <label
            htmlFor="tpl-desc"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Description
          </label>
          <textarea
            id="tpl-desc"
            rows={2}
            value={description}
            onChange={(e) => {
              setDescription(e.target.value);
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
            placeholder="Brief description of this template"
          />
        </div>

        <div>
          <label
            htmlFor="tpl-logo"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Logo URL
          </label>
          <input
            id="tpl-logo"
            type="text"
            value={logoUrl}
            onChange={(e) => {
              setLogoUrl(e.target.value);
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
            placeholder="https://example.com/icon.svg"
          />
        </div>

        <div>
          <label
            htmlFor="tpl-shape"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Shape
          </label>
          <select
            id="tpl-shape"
            value={shape}
            onChange={(e) => {
              setShape(e.target.value);
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
          >
            {AVAILABLE_SHAPES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="mb-1 block text-sm font-medium text-gray-700">
            Default OCPU: {ocpu}
          </label>
          <input
            type="range"
            min={1}
            max={64}
            value={ocpu}
            onChange={(e) => {
              setOcpu(Number(e.target.value));
            }}
            className="w-full accent-primary-600"
          />
          <div className="flex justify-between text-xs text-gray-400">
            <span>1</span>
            <span>64</span>
          </div>
        </div>

        <div>
          <label className="mb-1 block text-sm font-medium text-gray-700">
            Default Memory (GB): {memory}
          </label>
          <input
            type="range"
            min={1}
            max={1024}
            step={1}
            value={memory}
            onChange={(e) => {
              setMemory(Number(e.target.value));
            }}
            className="w-full accent-primary-600"
          />
          <div className="flex justify-between text-xs text-gray-400">
            <span>1 GB</span>
            <span>1024 GB</span>
          </div>
        </div>

        <div>
          <label
            htmlFor="tpl-boot"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Default Boot Volume (GB)
          </label>
          <input
            id="tpl-boot"
            type="number"
			min={50}
			max={200}
            value={bootVolume}
            onChange={(e) => {
              setBootVolume(Number(e.target.value));
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
          />
        </div>

        <div>
          <label
            htmlFor="tpl-cloudinit"
            className="mb-1 block text-sm font-medium text-gray-700"
          >
            Cloud-Init YAML
          </label>
          <textarea
            id="tpl-cloudinit"
            rows={12}
            value={cloudInitYaml}
            onChange={(e) => {
              setCloudInitYaml(e.target.value);
            }}
            className="w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
            placeholder="#cloud-config&#10;packages:&#10;  - nginx&#10;  - certbot&#10;runcmd:&#10;  - systemctl enable nginx"
            spellCheck={false}
          />
          <p className="mt-1 text-xs text-gray-400">
            Valid cloud-init YAML. Use <code>#cloud-config</code> header
            for standard cloud-init modules.
          </p>
        </div>

        <div className="flex justify-end gap-3 pt-2">
          <button
            type="button"
            onClick={() => {
              navigate("/vps/new");
            }}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={submitting}
            className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-semibold text-white transition-colors hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? "Saving..." : "Save Template"}
          </button>
        </div>
      </form>
    </div>
  );
}
