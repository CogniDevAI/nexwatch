import { AppWindow, Terminal, Laptop, HelpCircle, type LucideIcon } from "lucide-react";

/**
 * Maps an agent's `platform` field (runtime.GOOS — see
 * internal/shared/protocol.RegisterPayload's doc comment; "windows",
 * "linux", "darwin") to a small monochrome glyph shown next to the
 * human-readable `os` string in the Agents table and host detail header.
 * Deliberately generic device/terminal shapes rather than OS brand marks
 * (matching DESIGN.md's rejection of decorative icons that don't carry
 * information — this one distinguishes platform family at a glance the
 * same way StatusIndicator's shapes distinguish status).
 */
const PLATFORM_ICONS: Record<string, LucideIcon> = {
  windows: AppWindow,
  linux: Terminal,
  darwin: Laptop,
};

interface PlatformIconProps {
  platform: string | undefined;
  className?: string;
}

export function PlatformIcon({ platform, className = "h-3.5 w-3.5" }: PlatformIconProps) {
  const Icon = (platform && PLATFORM_ICONS[platform.toLowerCase()]) || HelpCircle;
  return (
    <Icon className={`shrink-0 text-[var(--color-ink-faint)] ${className}`} aria-hidden="true" />
  );
}
