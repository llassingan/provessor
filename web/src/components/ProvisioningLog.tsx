import { useEffect, useRef } from "react";
import type { SSEEvent } from "../hooks/useSSE";

interface ProvisioningLogProps {
  events: SSEEvent[];
  connected: boolean;
  vpsStatus?: string;
}

function statusColor(status: string): string {
  switch (status) {
    case "success":
    case "completed":
      return "bg-emerald-500";
    case "error":
    case "failed":
      return "bg-red-500";
    case "in_progress":
    case "running":
      return "bg-blue-500";
    default:
      return "bg-gray-400";
  }
}

function statusBg(status: string): string {
  switch (status) {
    case "success":
    case "completed":
      return "bg-emerald-50 border-emerald-200";
    case "error":
    case "failed":
      return "bg-red-50 border-red-200";
    case "in_progress":
    case "running":
      return "bg-blue-50 border-blue-200";
    default:
      return "bg-gray-50 border-gray-200";
  }
}

export default function ProvisioningLog({
  events,
  connected,
  vpsStatus,
}: ProvisioningLogProps): JSX.Element {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [events]);

  const isTerminal = vpsStatus === "failed" || vpsStatus === "running" || vpsStatus === "stopped" || vpsStatus === "terminated";

  if (events.length === 0 && isTerminal) {
    return (
      <div className={`rounded-lg border px-4 py-3 text-sm ${vpsStatus === "failed" ? "border-red-200 bg-red-50 text-red-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
        {vpsStatus === "failed" ? "Provisioning failed." : "Provisioning complete."}
      </div>
    );
  }

  if (events.length === 0) {
    return (
      <div className="rounded-lg border border-gray-200 bg-gray-50 p-6 text-center">
        <div
          className={`mx-auto mb-3 h-2 w-2 rounded-full ${connected ? "bg-blue-500 animate-pulse" : "bg-gray-300"}`}
        />
        <p className="text-sm text-gray-500">
          {connected
            ? "Waiting for events..."
            : "Not connected. Retrying..."}
        </p>
      </div>
    );
  }

  return (
    <div className="max-h-96 overflow-y-auto rounded-lg border border-gray-200 bg-white">
      <div className="divide-y divide-gray-100">
        {events.map((event, idx) => (
          <div
            key={`${event.timestamp}-${idx}`}
            className={`flex gap-3 px-4 py-3 text-sm ${statusBg(event.status)}`}
          >
            <div className="mt-1.5 flex flex-col items-center">
              <div
                className={`h-2.5 w-2.5 rounded-full ${statusColor(event.status)}`}
              />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex items-baseline gap-2">
                <span className="font-medium text-gray-900">
                  {event.step}
                </span>
                <span className="text-xs text-gray-400">
                  {new Date(event.timestamp).toLocaleTimeString()}
                </span>
              </div>
              <p className="mt-0.5 text-gray-600">{event.message}</p>
            </div>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
