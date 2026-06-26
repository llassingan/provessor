import type { Template } from "../lib/api";

interface TemplateCardProps {
  template: Template;
  selected: boolean;
  onSelect: (template: Template) => void;
}

const TEMPLATE_ICONS: Record<string, string> = {
  wordpress: "W",
  node: "N",
  docker: "D",
  ubuntu: "U",
};

export default function TemplateCard({
  template,
  selected,
  onSelect,
}: TemplateCardProps): JSX.Element {
  const initial = TEMPLATE_ICONS[template.name.toLowerCase()] ?? template.name.charAt(0).toUpperCase();

  return (
    <button
      type="button"
      onClick={() => {
        onSelect(template);
      }}
      className={`
        group relative flex flex-col items-start gap-3 rounded-xl border-2 p-5 text-left transition-all
        ${
          selected
            ? "border-primary-500 bg-primary-50 shadow-md"
            : "border-gray-200 bg-white hover:border-primary-300 hover:shadow-sm"
        }
      `}
    >
      {selected && (
        <div className="absolute right-3 top-3 flex h-6 w-6 items-center justify-center rounded-full bg-primary-500">
          <svg
            className="h-4 w-4 text-white"
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
      )}

      <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary-100 text-sm font-bold text-primary-700">
        {initial}
      </div>

      <div>
        <h3 className="text-sm font-semibold text-gray-900">
          {template.name}
        </h3>
        <p className="mt-1 text-xs text-gray-500">{template.description}</p>
      </div>

      <div className="flex flex-wrap gap-2 pt-1">
        <span className="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-600">
          {template.default_ocpu} OCPU
        </span>
        <span className="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-600">
          {template.default_memory} GB RAM
        </span>
        <span className="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-600">
          {template.boot_volume_size_gb} GB
        </span>
      </div>

      {template.type === "custom" && (
        <span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700">
          Custom
        </span>
      )}
    </button>
  );
}
