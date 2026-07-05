import { useState, useEffect, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { templates, vps, networks, shapes } from "../lib/api";
import type { Template, Network, ShapeGroup, ShapeSpec } from "../lib/api";
import TemplateCard from "../components/TemplateCard";

export default function NewVPS(): JSX.Element {
  const [step, setStep] = useState(1);
  const [allTemplates, setAllTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [loadingTemplates, setLoadingTemplates] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const [displayName, setDisplayName] = useState("");
  const [shapeGroups, setShapeGroups] = useState<ShapeGroup[]>([]);
  const [selectedGroup, setSelectedGroup] = useState<ShapeGroup | null>(null);
  const [selectedShape, setSelectedShape] = useState<ShapeSpec | null>(null);
  const [shape, setShape] = useState("");
  const [ocpu, setOcpu] = useState(1);
  const [memory, setMemory] = useState(4);
  const [bootVolume, setBootVolume] = useState(50);
  const [customPlaybook, setCustomPlaybook] = useState("");

  const [selectedNetwork, setSelectedNetwork] = useState<Network | null>(null);
  const [networkList, setNetworkList] = useState<Network[]>([]);
  const [loadingNetworks, setLoadingNetworks] = useState(true);
  const [showNoNetworkGuard, setShowNoNetworkGuard] = useState(false);

  const navigate = useNavigate();

  useEffect(() => {
    templates
      .list()
      .then(setAllTemplates)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Failed to load templates"))
      .finally(() => setLoadingTemplates(false));
  }, []);

  useEffect(() => {
    shapes.groups().then(setShapeGroups).catch(() => {});
  }, []);

  useEffect(() => {
    setLoadingNetworks(true);
    networks
      .list()
      .then((data) => setNetworkList(data.networks.filter((n) => n.status === "ready")))
      .catch(() => setNetworkList([]))
      .finally(() => setLoadingNetworks(false));
  }, []);

  useEffect(() => {
    if (step === 2 && !loadingNetworks && networkList.length === 0) {
      setShowNoNetworkGuard(true);
    }
  }, [step, loadingNetworks, networkList]);

  const handleSelectTemplate = (tpl: Template): void => {
    setSelectedTemplate(tpl);
    setDisplayName("");
    setShape(tpl.shape || "");
    setOcpu(tpl.default_ocpu);
    setMemory(tpl.default_memory);
    setBootVolume(tpl.boot_volume_size_gb);
    setCustomPlaybook("");

    const match = shapeGroups.flatMap((g) => g.shapes).find((s) => s.name === tpl.shape);
    if (match) {
      setSelectedShape(match);
      const parent = shapeGroups.find((g) => g.shapes.includes(match));
      setSelectedGroup(parent ?? null);
    } else {
      setSelectedShape(null);
      setSelectedGroup(null);
    }

    setStep(2);
  };

	const handleSelectGroup = (group: ShapeGroup): void => {
		setSelectedGroup(group);
		const first = group.shapes[0];
		if (first) {
			setSelectedShape(first);
			setShape(first.name);
			setOcpu(Math.min(1, first.max_ocpu));
			setMemory(Math.max(Math.min(4, first.max_memory), first.min_memory));
		}
	};

	const handleSelectShape = (s: ShapeSpec): void => {
		setSelectedShape(s);
		setShape(s.name);
		setOcpu(Math.min(ocpu, s.max_ocpu));
		setMemory(Math.min(Math.max(memory, s.min_memory), s.max_memory));
	};

  const handleSubmit = async (e: FormEvent): Promise<void> => {
    e.preventDefault();
    if (!selectedTemplate) return;

    if (displayName.trim().length === 0) {
      setError("Please enter a display name.");
      return;
    }
		if (ocpu < 1 || (selectedShape && ocpu > selectedShape.max_ocpu)) {
					setError(`OCPU must be between 1 and ${selectedShape?.max_ocpu ?? 64}.`);
					return;
				}
				if (memory < (selectedShape?.min_memory ?? 1) || (selectedShape && memory > selectedShape.max_memory)) {
					setError(`Memory must be between ${selectedShape?.min_memory ?? 1} and ${selectedShape?.max_memory ?? 1024} GB.`);
					return;
				}
				if (bootVolume < 50 || bootVolume > 200) {
					setError("Boot volume must be between 50 and 200 GB.");
					return;
    }
    if (!selectedNetwork) {
      setError("Please select a network.");
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      const created = await vps.create({
        template_id: selectedTemplate.id,
        network_id: selectedNetwork.id,
        display_name: displayName.trim(),
        shape,
        ocpu,
        memory_gb: memory,
        boot_volume_size_gb: bootVolume,
        custom_playbook_yaml: selectedTemplate.type === "custom" ? customPlaybook : undefined,
      });
      navigate(`/vps/${created.id}`);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to create VPS");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl">
      {showNoNetworkGuard && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl">
            <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-amber-100">
              <svg className="h-6 w-6 text-amber-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
              </svg>
            </div>
            <h3 className="mb-1 text-center text-lg font-semibold text-amber-900">Network Required</h3>
            <p className="mb-4 text-center text-sm text-amber-700">
              You don&apos;t have any active network yet. Create a network before provisioning a VPS.
            </p>
            <div className="flex justify-center gap-3">
              <button type="button" onClick={() => navigate("/networks/new")} className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700">
                Create network
              </button>
              <button type="button" onClick={() => setShowNoNetworkGuard(false)} className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50">
                I&apos;ll do it later
              </button>
            </div>
          </div>
        </div>
      )}
      <h1 className="mb-1 text-2xl font-bold text-gray-900">New VPS Instance</h1>
      <p className="mb-6 text-sm text-gray-500">Deploy a new cloud instance in minutes</p>

      {error && (
        <div className="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
      )}

      <div className="mb-8 flex items-center gap-2">
        {[1, 2, 3].map((s) => (
          <div key={s} className="flex items-center gap-2">
            <div className={`flex h-8 w-8 items-center justify-center rounded-full text-sm font-semibold ${step >= s ? "bg-primary-600 text-white" : "bg-gray-200 text-gray-500"}`}>
              {step > s ? (
                <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                </svg>
              ) : s}
            </div>
            <span className={`text-sm font-medium ${step >= s ? "text-gray-900" : "text-gray-400"}`}>
              {s === 1 ? "Template" : s === 2 ? "Configure" : "Review"}
            </span>
            {s < 3 && <div className="h-px w-8 bg-gray-200" />}
          </div>
        ))}
      </div>

      {/* Step 1: Template */}
      {step === 1 && (
        <div>
          <h2 className="mb-4 text-lg font-semibold text-gray-900">Choose a template</h2>
          {loadingTemplates ? (
            <div className="grid gap-4 sm:grid-cols-2">
              {Array.from({ length: 4 }, (_, i) => (
                <div key={i} className="animate-pulse rounded-xl border border-gray-200 p-5">
                  <div className="mb-3 h-10 w-10 rounded-lg bg-gray-200" />
                  <div className="mb-2 h-4 w-1/2 rounded bg-gray-200" />
                  <div className="h-3 w-2/3 rounded bg-gray-100" />
                </div>
              ))}
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2">
              {allTemplates.map((tpl) => (
                <TemplateCard key={tpl.id} template={tpl} selected={selectedTemplate?.id === tpl.id} onSelect={handleSelectTemplate} />
              ))}
            </div>
          )}
          <div className="mt-6 flex justify-between">
            <button type="button" onClick={() => navigate("/dashboard")} className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50">
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Step 2: Configuration */}
      {step === 2 && selectedTemplate && (
        <form onSubmit={(e) => { e.preventDefault(); setStep(3); }}>
          <h2 className="mb-4 text-lg font-semibold text-gray-900">Configure instance</h2>

          <div className="space-y-5 rounded-xl border border-gray-200 bg-white p-6">
            <div>
              <label htmlFor="name" className="mb-1 block text-sm font-medium text-gray-700">Display Name</label>
              <input id="name" type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)} className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" placeholder="my-production-server" required />
            </div>

            {/* Two-level shape selector */}
            <div>
              <label className="mb-2 block text-sm font-medium text-gray-700">Processor Group</label>
              <div className="mb-4 grid gap-3 sm:grid-cols-3">
                {shapeGroups.map((group) => (
                  <button
                    key={group.label}
                    type="button"
                    onClick={() => handleSelectGroup(group)}
                    className={`rounded-xl border-2 px-4 py-3 text-left transition-all ${
                      selectedGroup?.label === group.label
                        ? "border-primary-500 bg-primary-50 shadow-sm"
                        : "border-gray-200 bg-white hover:border-gray-300"
                    }`}
                  >
                    <div className="text-sm font-semibold text-gray-900">{group.label}</div>
                    <div className="mt-0.5 text-xs text-gray-500">{group.description}</div>
                  </button>
                ))}
              </div>

              {selectedGroup && (
                <>
                  <label className="mb-2 block text-sm font-medium text-gray-700">Shape</label>
                  <div className="grid gap-3 sm:grid-cols-2">
                    {selectedGroup.shapes.map((s) => (
                      <button
                        key={s.name}
                        type="button"
                        onClick={() => handleSelectShape(s)}
                        className={`rounded-xl border-2 px-4 py-3 text-left transition-all ${
                          selectedShape?.name === s.name
                            ? "border-primary-500 bg-primary-50 shadow-sm"
                            : "border-gray-200 bg-white hover:border-gray-300"
                        }`}
                      >
                        <div className="mb-1 font-mono text-sm font-medium text-gray-900">{s.name}</div>
                        <div className="text-xs text-gray-500">{s.processor}</div>
                        <div className="mt-1.5 flex flex-wrap gap-2 text-xs">
                          <span className="rounded-full bg-gray-100 px-2 py-0.5 text-gray-600">{s.min_ocpu}–{s.max_ocpu} OCPU</span>
                          <span className="rounded-full bg-gray-100 px-2 py-0.5 text-gray-600">{s.min_memory}–{s.max_memory} GB</span>
                          <span className="rounded-full bg-gray-100 px-2 py-0.5 text-gray-600">{s.max_network}</span>
                        </div>
                      </button>
                    ))}
                  </div>
                </>
              )}
            </div>

            <div>
              <label className="mb-1 block text-sm font-medium text-gray-700">
                OCPU
              </label>
              <input type="number" min={1} max={selectedShape?.max_ocpu ?? 64} value={ocpu} onChange={(e) => setOcpu(Math.max(1, Math.min(selectedShape?.max_ocpu ?? 64, Number(e.target.value) || 1)))} className="mb-2 w-20 rounded-lg border border-gray-300 px-2 py-1.5 text-sm text-center focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" />
              <div className="flex items-center gap-3">
                <input type="range" min={1} max={selectedShape?.max_ocpu ?? 64} value={ocpu} onChange={(e) => setOcpu(Number(e.target.value))} className="flex-1 accent-primary-600" />
              </div>
              <div className="flex justify-between text-xs text-gray-400">
                <span>1</span>
                <span>{selectedShape?.max_ocpu ?? 64}</span>
              </div>
            </div>

            <div>
              <label className="mb-1 block text-sm font-medium text-gray-700">
                Memory (GB)
              </label>
              <input type="number" min={selectedShape?.min_memory ?? 1} max={selectedShape?.max_memory ?? 1024} value={memory} onChange={(e) => setMemory(Math.max(selectedShape?.min_memory ?? 1, Math.min(selectedShape?.max_memory ?? 1024, Number(e.target.value) || 1)))} className="mb-2 w-20 rounded-lg border border-gray-300 px-2 py-1.5 text-sm text-center focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" />
              <div className="flex items-center gap-3">
                <input type="range" min={selectedShape?.min_memory ?? 1} max={selectedShape?.max_memory ?? 1024} step={1} value={memory} onChange={(e) => setMemory(Number(e.target.value))} className="flex-1 accent-primary-600" />
              </div>
              <div className="flex justify-between text-xs text-gray-400">
                <span>{selectedShape?.min_memory ?? 1} GB</span>
                <span>{selectedShape?.max_memory ?? 1024} GB</span>
              </div>
            </div>

            <div>
              <label htmlFor="boot" className="mb-1 block text-sm font-medium text-gray-700">Boot Volume (GB)</label>
              <input id="boot" type="number" min={50} max={200} value={bootVolume} onChange={(e) => setBootVolume(Number(e.target.value))} className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" />
            </div>

            <div>
              <label htmlFor="network" className="mb-1 block text-sm font-medium text-gray-700">Network</label>
              {loadingNetworks ? (
                <select id="network" disabled className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-400">
                  <option>Loading networks...</option>
                </select>
              ) : networkList.length === 0 ? (
                <select id="network" disabled className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-400">
                  <option>No ready networks</option>
                </select>
              ) : (
                <select id="network" value={selectedNetwork?.id ?? ""} onChange={(e) => {
                  const id = Number(e.target.value);
                  setSelectedNetwork(networkList.find((n) => n.id === id) ?? null);
                }} className="w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" required>
                  <option value="" disabled>Select a network</option>
                  {networkList.map((net) => (
                    <option key={net.id} value={net.id}>{net.name} ({net.region} · {net.cidr_vcn})</option>
                  ))}
                </select>
              )}
            </div>

            {selectedTemplate.type === "custom" && (
              <div>
                <label htmlFor="playbook" className="mb-1 block text-sm font-medium text-gray-700">Ansible Playbook (YAML)</label>
                <textarea id="playbook" rows={6} value={customPlaybook} onChange={(e) => setCustomPlaybook(e.target.value)} className="w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm focus:border-primary-500 focus:outline-none focus:ring-1 focus:ring-primary-500" placeholder="---&#10;- hosts: all&#10;  tasks:&#10;    - name: Install nginx&#10;      apt:&#10;        name: nginx&#10;        state: present" />
              </div>
            )}
          </div>

          <div className="mt-6 flex justify-between">
            <button type="button" onClick={() => setStep(1)} className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50">
              Back
            </button>
            <button type="submit" disabled={networkList.length === 0} className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-medium text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50">
              Review
            </button>
          </div>
        </form>
      )}

      {/* Step 3: Review */}
      {step === 3 && selectedTemplate && (
        <div>
          <h2 className="mb-4 text-lg font-semibold text-gray-900">Review &amp; Launch</h2>

          <div className="rounded-xl border border-gray-200 bg-white p-6">
            <dl className="space-y-4">
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Template</dt>
                <dd className="text-sm font-medium text-gray-900">{selectedTemplate.name}</dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Display Name</dt>
                <dd className="text-sm font-medium text-gray-900">{displayName}</dd>
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
                <dd className="text-sm font-medium text-gray-900">{memory} GB</dd>
              </div>
              <div className="flex justify-between border-b border-gray-100 pb-3">
                <dt className="text-sm text-gray-500">Boot Volume</dt>
                <dd className="text-sm font-medium text-gray-900">{bootVolume} GB</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-gray-500">Network</dt>
                <dd className="text-sm font-medium text-gray-900">{selectedNetwork?.name ?? "—"} ({selectedNetwork?.region ?? ""})</dd>
              </div>
            </dl>
          </div>

          <div className="mt-6 flex justify-between">
            <button type="button" onClick={() => setStep(2)} className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50">
              Back
            </button>
            <button type="button" onClick={(e) => { void handleSubmit(e); }} disabled={submitting} className="rounded-lg bg-primary-600 px-6 py-2 text-sm font-semibold text-white hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50">
              {submitting ? "Launching..." : "Launch Instance"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
