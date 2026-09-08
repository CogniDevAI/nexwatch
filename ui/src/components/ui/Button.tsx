import type { ButtonHTMLAttributes } from "react";

type Variant = "primary" | "secondary" | "accent" | "danger" | "ghost";
type Size = "sm" | "md";

const BASE =
  "inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] font-medium whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-50";

const VARIANT_CLASS: Record<Variant, string> = {
  primary: "bg-[var(--color-signal)] text-[var(--color-void)] hover:opacity-90",
  secondary:
    "border border-[var(--color-line)] bg-[var(--color-panel-raised)] text-[var(--color-ink)] hover:border-[var(--color-ink-faint)]",
  accent:
    "border border-[var(--color-signal)]/25 bg-[var(--color-signal)]/10 text-[var(--color-signal)] hover:bg-[var(--color-signal)]/20",
  danger: "bg-[var(--color-critical)] text-white hover:opacity-90",
  ghost:
    "text-[var(--color-ink-muted)] hover:bg-[var(--color-panel-raised)] hover:text-[var(--color-ink)]",
};

const SIZE_CLASS: Record<Size, string> = {
  sm: "px-2.5 py-1.5 text-xs",
  md: "px-4 py-2 text-sm",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

/** Named button variants — replaces one-off inline button class strings. See DESIGN.md §7. */
export function Button({
  variant = "secondary",
  size = "md",
  type = "button",
  className = "",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`${BASE} ${VARIANT_CLASS[variant]} ${SIZE_CLASS[size]} ${className}`}
      {...rest}
    />
  );
}

type IconButtonProps = Omit<ButtonProps, "size">;

/** Icon-only button — caller MUST pass an aria-label. Sized independently of
 *  <Button> so its fixed square padding never competes with SIZE_CLASS. */
export function IconButton({
  variant = "ghost",
  type = "button",
  className = "",
  ...rest
}: IconButtonProps) {
  return (
    <button
      type={type}
      className={`${BASE} p-1.5 ${VARIANT_CLASS[variant]} ${className}`}
      {...rest}
    />
  );
}
