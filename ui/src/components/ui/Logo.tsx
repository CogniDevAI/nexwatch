/** The NexWatch mark: a signal pulse (an uptime/ping trace), echoing the
 *  Signal Rail concept. See DESIGN.md §4. */
export function LogoMark({ className = "h-6 w-6" }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect width="32" height="32" rx="7" fill="var(--color-panel-raised)" />
      <path
        d="M4 18h4l2.4-7.2c.3-.9 1.6-.9 1.9 0L15.5 21c.3.9 1.6.9 1.9 0l2.7-8.1c.3-.9 1.6-.9 1.9 0L23.4 18H28"
        fill="none"
        stroke="var(--color-signal)"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function Wordmark({ className = "" }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      <LogoMark className="h-7 w-7 flex-shrink-0" />
      <span className="text-lg font-semibold tracking-tight text-[var(--color-ink)]">NexWatch</span>
    </span>
  );
}
