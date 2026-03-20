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

export function TimeRangeSelector({
  selected,
  onChange,
}: TimeRangeSelectorProps) {
  function handleClick(option: TimeRangeOption) {
    const end = Math.floor(Date.now() / 1000);
    const start = end - option.duration;
    onChange({ value: option.value, start, end });
  }

  return (
    <div className="inline-flex rounded-lg border border-[var(--color-border-default)] bg-[var(--color-bg-surface)] p-1 gap-0.5">
      {TIME_RANGES.map((option) => (
        <button
          key={option.value}
          onClick={() => handleClick(option)}
          className={`px-3 py-1.5 rounded-md text-xs font-medium transition-all duration-150 ${
            selected === option.value
              ? "bg-[var(--color-accent-cyan)]/15 text-[var(--color-accent-cyan)] shadow-sm"
              : "text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-bg-elevated)]"
          }`}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
