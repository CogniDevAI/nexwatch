import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import {
  Activity,
  CheckCircle2,
  Eye,
  EyeOff,
  LockKeyhole,
  RadioTower,
  Server,
  ShieldCheck,
} from "lucide-react";
import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import { Wordmark } from "@/components/ui/Logo";
import { Input, Label } from "@/components/ui/Field";
import { Button } from "@/components/ui/Button";
import { usePageTitle } from "@/hooks/usePageTitle";

type HealthState = "checking" | "online" | "offline";

function healthLabel(status: HealthState): string {
  switch (status) {
    case "checking":
      return "Checking";
    case "online":
      return "Healthy";
    case "offline":
      return "Unavailable";
  }
}

function authErrorMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim()) {
    const message = err.message.toLowerCase();
    if (message.includes("failed to fetch") || message.includes("network")) {
      return "Cannot reach the NexWatch Hub. Check the endpoint and try again.";
    }
    if (message.includes("400") || message.includes("403") || message.includes("invalid")) {
      return "The credentials were rejected. Check the email, password, and user role.";
    }
  }
  return "Sign-in failed. Check the Hub connection and credentials.";
}

export function Login() {
  usePageTitle("Sign in");

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [health, setHealth] = useState<HealthState>("checking");

  const login = useAuthStore((s) => s.login);
  const navigate = useNavigate();
  const isDev = import.meta.env.DEV;
  const hubEndpoint = pb.baseUrl;

  useEffect(() => {
    const controller = new AbortController();

    async function checkHealth() {
      setHealth("checking");
      try {
        const response = await fetch(`${hubEndpoint}/healthz`, { signal: controller.signal });
        setHealth(response.ok ? "online" : "offline");
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setHealth("offline");
      }
    }

    void checkHealth();
    return () => controller.abort();
  }, [hubEndpoint]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      await login(email, password);
      void navigate("/", { replace: true });
    } catch (err) {
      setError(authErrorMessage(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-[var(--color-void)] text-[var(--color-ink)]">
      <div className="pointer-events-none absolute inset-0 opacity-80">
        <div className="absolute top-[-22rem] left-[-18rem] h-[42rem] w-[42rem] rounded-full bg-[var(--color-signal)]/10 blur-3xl" />
        <div className="absolute right-[-12rem] bottom-[-20rem] h-[36rem] w-[36rem] rounded-full bg-[var(--color-ok)]/8 blur-3xl" />
        <div className="absolute inset-0 bg-[linear-gradient(rgba(91,157,255,0.045)_1px,transparent_1px),linear-gradient(90deg,rgba(91,157,255,0.035)_1px,transparent_1px)] [mask-image:radial-gradient(circle_at_42%_30%,black,transparent_72%)] bg-[size:44px_44px]" />
      </div>

      <main className="relative mx-auto grid min-h-screen w-full max-w-7xl grid-cols-1 lg:grid-cols-[minmax(0,1fr)_28rem]">
        <section className="flex min-h-[48vh] flex-col justify-between px-6 py-7 sm:px-10 lg:min-h-screen lg:px-12 lg:py-10">
          <div className="flex items-center justify-between gap-4">
            <Wordmark />
            <div className="hidden items-center gap-2 text-xs text-[var(--color-ink-faint)] sm:flex">
              <span
                className={`h-2 w-2 rounded-full ${
                  health === "online"
                    ? "bg-[var(--color-ok)]"
                    : health === "offline"
                      ? "bg-[var(--color-critical)]"
                      : "bg-[var(--color-ink-faint)]"
                }`}
                aria-hidden="true"
              />
              Hub {healthLabel(health).toLowerCase()}
            </div>
          </div>

          <div className="max-w-3xl py-14 lg:py-0">
            <div className="mb-8 inline-flex items-center gap-2 border-l-2 border-[var(--color-signal)] bg-[var(--color-signal)]/8 px-3 py-2 text-xs text-[var(--color-ink-muted)]">
              <RadioTower className="h-4 w-4 text-[var(--color-signal)]" aria-hidden="true" />
              Operator access · live hub connection
            </div>

            <h1 className="max-w-4xl text-5xl leading-[0.98] font-semibold tracking-[-0.055em] text-[var(--color-ink)] sm:text-6xl lg:text-7xl">
              See what needs attention before everything looks normal.
            </h1>
            <p className="mt-7 max-w-2xl text-base leading-7 text-[var(--color-ink-muted)] sm:text-lg">
              NexWatch opens into a monitoring workspace for hosts, checks, alerts, logs, and
              maintenance windows. The login should get operators into that flow fast — no marketing
              detour, no fake metrics.
            </p>
          </div>

          <div className="grid max-w-3xl gap-px overflow-hidden rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-line)] sm:grid-cols-3">
            {[
              ["Observe", "Fleet posture and checks"],
              ["Investigate", "Host metrics and logs"],
              ["Respond", "Acknowledge and silence"],
            ].map(([label, value]) => (
              <div key={label} className="bg-[var(--color-void)]/92 p-4">
                <p className="text-xs text-[var(--color-ink-faint)]">{label}</p>
                <p className="mt-2 text-sm font-semibold text-[var(--color-ink)]">{value}</p>
              </div>
            ))}
          </div>
        </section>

        <aside className="flex items-center border-t border-[var(--color-line)] bg-[var(--color-panel)]/78 px-6 py-8 backdrop-blur-xl sm:px-10 lg:min-h-screen lg:border-t-0 lg:border-l lg:px-8">
          <div className="w-full space-y-6">
            <div className="space-y-3">
              <div className="flex items-start gap-3 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-void)] px-4 py-3">
                <Server className="mt-0.5 h-4 w-4 text-[var(--color-signal)]" aria-hidden="true" />
                <div className="min-w-0 flex-1">
                  <p className="text-xs text-[var(--color-ink-faint)]">Hub endpoint</p>
                  <p className="mt-1 truncate font-mono text-sm text-[var(--color-ink)]">
                    {hubEndpoint}
                  </p>
                </div>
              </div>

              <div className="flex items-center justify-between border-y border-[var(--color-line)] py-3">
                <span className="inline-flex items-center gap-2 text-sm text-[var(--color-ink-muted)]">
                  <Activity className="h-4 w-4" aria-hidden="true" />
                  Hub health
                </span>
                <span
                  className={`inline-flex items-center gap-1.5 font-mono text-xs ${
                    health === "online"
                      ? "text-[var(--color-ok)]"
                      : health === "offline"
                        ? "text-[var(--color-critical)]"
                        : "text-[var(--color-ink-faint)]"
                  }`}
                >
                  {health === "online" && (
                    <CheckCircle2 className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  {healthLabel(health)}
                </span>
              </div>
            </div>

            <form onSubmit={handleSubmit} className="space-y-5" aria-label="Sign in to NexWatch">
              <div>
                <div className="mb-6 flex items-center gap-3">
                  <div className="flex h-11 w-11 items-center justify-center rounded-[var(--radius-control)] bg-[var(--color-signal)] text-[var(--color-void)]">
                    <LockKeyhole className="h-5 w-5" aria-hidden="true" />
                  </div>
                  <div>
                    <h2 className="text-2xl font-semibold tracking-[-0.03em] text-[var(--color-ink)]">
                      Open console
                    </h2>
                    <p className="text-sm text-[var(--color-ink-muted)]">
                      Use your NexWatch operator account.
                    </p>
                  </div>
                </div>

                {error && (
                  <div
                    role="alert"
                    className="mb-4 border-l-2 border-[var(--color-critical)] bg-[var(--color-critical)]/10 px-4 py-3 text-sm text-[var(--color-critical)]"
                  >
                    {error}
                  </div>
                )}

                <div className="space-y-4">
                  <div>
                    <Label htmlFor="email">Email</Label>
                    <Input
                      id="email"
                      type="email"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      required
                      autoComplete="email"
                      placeholder="admin@nexwatch.local"
                    />
                  </div>

                  <div>
                    <Label htmlFor="password">Password</Label>
                    <div className="relative">
                      <Input
                        id="password"
                        type={showPassword ? "text" : "password"}
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        required
                        autoComplete="current-password"
                        placeholder="Enter your password"
                        className="pr-10"
                      />
                      <button
                        type="button"
                        onClick={() => setShowPassword(!showPassword)}
                        aria-label={showPassword ? "Hide password" : "Show password"}
                        className="absolute top-1/2 right-3 -translate-y-1/2 rounded-[var(--radius-control)] text-[var(--color-ink-faint)] transition-colors hover:text-[var(--color-ink-muted)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[var(--color-signal)]"
                      >
                        {showPassword ? (
                          <EyeOff className="h-4 w-4" />
                        ) : (
                          <Eye className="h-4 w-4" />
                        )}
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {isDev && (
                <div className="border border-dashed border-[var(--color-line)] bg-[var(--color-void)]/65 px-3 py-2 text-xs leading-5 text-[var(--color-ink-faint)]">
                  Local development credentials:{" "}
                  <span className="font-mono">admin@nexwatch.local</span> /{" "}
                  <span className="font-mono">admin123456</span>
                </div>
              )}

              <Button
                type="submit"
                variant="primary"
                disabled={loading}
                className="w-full !py-3 text-sm font-semibold"
              >
                {loading ? "Opening console…" : "Enter NexWatch"}
              </Button>
            </form>

            <p className="flex items-start gap-2 text-xs leading-5 text-[var(--color-ink-faint)]">
              <ShieldCheck className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" aria-hidden="true" />
              Authentication is handled by the connected Hub. This screen only verifies access and
              routes you into the operations workspace.
            </p>
          </div>
        </aside>
      </main>
    </div>
  );
}
