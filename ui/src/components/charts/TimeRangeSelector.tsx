interface TimeRangeOption {
  label: string;
  value: string;
  /** Duration in seconds */
  duration: number;
}

const TIME_RANGES: TimeRangeOption[] = [
  { label: "1h", value: "1h", duration: 3600 },
  { label: "6h", value: "6h", duration: 21600 },
  { label: "24h", value: "24h", duration: 86400 },
  { label: "7d", value: "7d", duration: 604800 },
  { label: "30d", value: "30d", duration: 2592000 },
];

interface TimeRangeSelectorProps {
  selected: string;
  onChange: (range: { value: string; start: number; end: number }) => void;
}

export function TimeRangeSelector({ selected, onChange }: TimeRangeSelectorProps) {
  function handleClick(option: TimeRangeOption) {
    const end = Math.floor(Date.now() / 1000);
    const start = end - option.duration;
    onChange({ value: option.value, start, end });
  }

  return (
    <div
      role="radiogroup"
      aria-label="Time range"
      className="inline-flex gap-0.5 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] p-1"
    >
      {TIME_RANGES.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={selected === option.value}
          onClick={() => handleClick(option)}
          className={`rounded-[var(--radius-chip)] px-3 py-1.5 text-xs font-medium transition-colors ${
            selected === option.value
              ? "bg-[var(--color-signal)]/15 text-[var(--color-signal)]"
              : "text-[var(--color-ink-muted)] hover:bg-[var(--color-panel-raised)] hover:text-[var(--color-ink)]"
          }`}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
