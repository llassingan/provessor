import { useState, useEffect, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { templates, vps } from "../lib/api";
import type { Template } from "../lib/api";
import TemplateCard from "../components/TemplateCard";

const AVAILABLE_SHAPES = [
  "VM.Standard.E4.Flex",
  "VM.Standard.E3.Flex",
  "VM.Standard.A1.Flex",
];

export default function NewVPS(): JSX.Element {
  const [step, setStep] = useState(1);
  const [allTemplates, setAllTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [loadingTemplates, setLoadingTemplates] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const [displayName, setDisplayName] = useState("");
  const [shape, setShape] = useState(AVAILABLE_SHAPES[0] ?? "");
  const [ocpu, setOcpu] = useState(1);
  const [memory, setMemory] = useState(4);
  const [bootVolume, setBootVolume] = useState(50);
  const [customPlaybook, setCustomPlaybook] = useState("");

  const navigate = useNavigate();

  useEffect(() => {
    templates
      .list()
      .then((data) => {
        setAllTemplates(data);
      })
      .catch((err: unknown) => {
        setError(
          err instanceof Error ? err.message : "Failed to load templates",
        );
      })
      .finally(() => {
        setLoadingTemplates(false);
      });
  }, []);

  const handleSelectTemplate = (tpl: Template): void => {
    setSelectedTemplate(tpl);
    setDisplayName("");
    setShape(tpl.shape || (AVAILABLE_SHAPES[0] ?? "VM.Standard.E4.Flex"));
    setOcpu(tpl.default_ocpu);
    setMemory(tpl.default_memory);
    setBootVolume(tpl.boot_volume_size_gb);
    setCustomPlaybook("");
    setStep(2);
  };

  const handleSubmit = async (e: FormEvent): Promise<void> => {
    e.preventDefault();
    if (!selectedTemplate) return;

    if (displayName.trim().length === 0) {
      setError("Please enter a display name.");
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
    if (bootVolume < 10 || bootVolume > 200) {
      setError("Boot volume must be between 10 and 200 GB.");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      const created = await vps.create({
        template_id: selectedTemplate.id,
        display_name: displayName.trim(),
        shape,
        ocpu,
        memory_gb: memory,
        boot_volume_size_gb: bootVolume,
        custom_playbook_yaml:
          selectedTemplate.type === "custom" ? customPlaybook : undefined,
      });
      navigate(`/vps/${created.id}`);
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to create VPS",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl">
      <h1 className="mb-1 text-2xl font-bold text-gray-900">
        New VPS Instance
      </h1>
      <p className="mb-6 text-sm text-gray-500">
        Deploy a new cloud instance in minutes
      </p>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          {error}
        </div>
      )}

      {/* Step indicator */}
      <div className="mb-8 flex items-center gap-2">
        {[1, 2, 3].map((s) => (
          <div key={s} className="flex items-center gap-2">
            <div
              className={`flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold ${
                step >= s
                  ? "bg-primary-600 text-white"
                  : "bg-gray-200 text-gray-500"
              }`}
            >
              {step > s ? (
                <svg
                  className="h-4 w-4"
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
              ) : (
                s
              )}
            </div>
            <span
              className={`text-sm font-medium ${step >= s ? "text-gray-900" : "text-gray-400"}`}
            >
              {s === 1 ? "Template" : s === 2 ? "Configure" : "Review"}
            </span>
            {s < 3 && <div className="h-px w-8 bg-gray-200" />}
          </div>
        ))}
      </div>

      {/* Step 1: Template */}
      {step === 1 && (
        <div>
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Choose a template
          </h2>
          {loadingTemplates ? (
            <div className="grid gap-4 sm:grid-cols-2">
              {Array.from({ length: 4 }, (_, i) => (
                <div
                  key={i}
                  className="animate-pulse rounded-xl border border-gray-200 p-5"
                >
                  <div className="mb-3 h-10 w-10 rounded-lg bg-gray-200" />
                  <div className="mb-2 h-4 w-1/2 rounded bg-gray-200" />
                  <div className="h-3 w-2/3 rounded bg-gray-100" />
                </div>
              ))}
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2">
              {allTemplates.map((tpl) => (
                <TemplateCard
                  key={tpl.id}
                  template={tpl}
                  selected={selectedTemplate?.id === tpl.id}
                  onSelect={handleSelectTemplate}
                />
              ))}
            </div>
          )}

          <div className="mt-6 flex justify-between">
            <button
              type="button"
              onClick={() => {
                navigate("/dashboard");
              }}
              className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Step 2: Configuration */}
      {step === 2 && selectedTemplate && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setStep(3);
          }}
        >
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Configure instance
          </h2>

          <div className="space-y-5 rounded-xl border border-gray-200 bg-white p-6">
            <div>
              <label
                htmlFor="name"
                className="mb-1 block text-sm font-medium text-gray-700"
              >
                Display Name
              </label>
              <input
                id="name"
                type="text"
                value={displayName}
                onChange={(e) => {
                  setDisplayName(e.target.value);
                }}
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
                placeholder="my-production-server"
                required
              />
            </div>

            <div>
              <label
                htmlFor="shape"
                className="mb-1 block text-sm font-medium text-gray-700"
              >
                Shape
              </label>
              <select
                id="shape"
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
                OCPU: {ocpu}
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
                Memory (GB): {memory}
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
                htmlFor="boot"
                className="mb-1 block text-sm font-medium text-gray-700"
              >
                Boot Volume (GB)
              </label>
              <input
                id="boot"
                type="number"
                min={10}
                max={200}
                value={bootVolume}
                onChange={(e) => {
                  setBootVolume(Number(e.target.value));
                }}
                className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
              />
            </div>

            {selectedTemplate.type === "custom" && (
              <div>
                <label
                  htmlFor="playbook"
                  className="mb-1 block text-sm font-medium text-gray-700"
                >
                  Ansible Playbook (YAML)
                </label>
                <textarea
                  id="playbook"
                  rows={6}
                  value={customPlaybook}
                  onChange={(e) => {
                    setCustomPlaybook(e.target.value);
                  }}
                  className="w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500"
                  placeholder="---&#10;- hosts: all&#10;  tasks:&#10;    - name: Install nginx&#10;      apt:&#10;        name: nginx&#10;        state: present"
                />
              </div>
            )}
          </div>

          <div className="mt-6 flex justify-between">
            <button
              type="button"
              onClick={() => {
                setStep(1);
              }}
              className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Back
            </button>
            <button
              type="submit"
              className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700"
            >
              Review
            </button>
          </div>
        </form>
      )}

      {/* Step 3: Review */}
      {step === 3 && selectedTemplate && (
        <div>
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Review &amp; Launch
          </h2>

          <div className="rounded-xl border border-gray-200 bg-white p-6">
            <dl className="space-y-4">
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Template</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {selectedTemplate.name}
                </dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Display Name</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {displayName}
                </dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Shape</dt>
                <dd className="text-sm font-medium text-gray-900">{shape}</dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">OCPU</dt>
                <dd className="text-sm font-medium text-gray-900">{ocpu}</dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Memory</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {memory} GB
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Boot Volume</dt>
                <dd className="text-sm font-medium text-gray-900">
                  {bootVolume} GB
                </dd>
              </div>
            </dl>
          </div>

          <div className="mt-6 flex justify-between">
            <button
              type="button"
              onClick={() => {
                setStep(2);
              }}
              className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
            >
              Back
            </button>
            <button
              type="button"
              onClick={(e) => {
                void handleSubmit(e);
              }}
              disabled={submitting}
              className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-semibold text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {submitting ? "Launching..." : "Launch Instance"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
