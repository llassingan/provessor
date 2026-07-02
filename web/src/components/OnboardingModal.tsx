interface OnboardingModalProps {
  onDismiss: () => void;
  onGoToSettings: () => void;
}

export default function OnboardingModal({
  onDismiss,
  onGoToSettings,
}: OnboardingModalProps): JSX.Element {
  function handleDismiss(): void {
    localStorage.setItem("onboarding_dismissed", "1");
    onDismiss();
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl">
        <h2 className="text-lg font-semibold text-gray-900">
          Welcome to Provessor
        </h2>
        <p className="mt-1 text-sm text-gray-500">
          Get your cloud infrastructure ready in three steps — or skip and
          explore the dashboard first.
        </p>

        <div className="mt-6 space-y-4">
          <Step
            number={1}
            label="Add OCI Credentials"
            description="Enter your Oracle Cloud credentials so Provessor can manage your resources."
          />
          <Step
            number={2}
            label="Setup Network"
            description="Provision a VCN and subnet for your VPS instances."
          />
          <Step
            number={3}
            label="Provision VPS"
            description="Choose a template and launch your first VPS instance."
          />
        </div>

        <div className="mt-6 flex gap-3">
          <button
            type="button"
            onClick={onGoToSettings}
            className="flex-1 rounded-lg bg-primary-600 px-4 py-2.5 text-sm font-semibold text-white transition-colors hover:bg-primary-700"
          >
            Set up now
          </button>
          <button
            type="button"
            onClick={handleDismiss}
            className="rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50"
          >
            I&apos;ll set this up later
          </button>
        </div>
      </div>
    </div>
  );
}

function Step({
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
