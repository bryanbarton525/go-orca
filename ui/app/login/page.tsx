import Image from "next/image";
import { redirect } from "next/navigation";
import { SignInButton } from "../../components/sign-in-button";
import { ThemeToggle } from "../../components/theme-toggle";
import { getSession } from "../../lib/auth/session";

export default async function LoginPage({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  const session = await getSession();
  if (session) {
    redirect("/overview");
  }

  const params = (await searchParams) ?? {};
  const errorParam = params.error;
  const error = Array.isArray(errorParam) ? errorParam[0] : errorParam;

  return (
    <main className="relative min-h-screen overflow-hidden px-6 py-10 md:px-10">
      <div className="pointer-events-none absolute inset-0 grid-ornament opacity-60" />
      <div className="mx-auto flex max-w-6xl justify-end pb-4">
        <ThemeToggle />
      </div>
      <div className="mx-auto grid min-h-[calc(100vh-5rem)] max-w-6xl items-center gap-10 lg:grid-cols-[1.15fr_0.85fr]">
        <section className="space-y-8">
          <div className="space-y-4">
            <div className="flex items-center gap-4">
              <Image
                src="/go-orca.png"
                alt="go-orca"
                width={72}
                height={72}
                className="rounded-2xl shadow-aura"
                priority
              />
              <p className="eyebrow">Homelab Portal Link</p>
            </div>
            <h1 className="max-w-3xl font-display text-5xl font-bold tracking-tight text-ink sm:text-6xl">
              Operate go-orca from a secure control surface instead of a raw API.
            </h1>
            <p className="max-w-2xl text-lg text-shell-muted">
              This interface sits behind Authentik and the iambarton.com gateway so you can manage workflows,
              tenants, scopes, providers, and streaming execution safely from desktop or mobile.
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <div className="glass-panel rounded-3xl p-5 shadow-aura">
              <p className="eyebrow">Workflows</p>
              <p className="mt-3 text-sm text-shell-muted">
                Create, inspect, resume, cancel, and stream multi-phase workflow runs.
              </p>
            </div>
            <div className="glass-panel rounded-3xl p-5 shadow-aura">
              <p className="eyebrow">Scoping</p>
              <p className="mt-3 text-sm text-shell-muted">
                Switch tenant and scope context without dropping to curl.
              </p>
            </div>
            <div className="glass-panel rounded-3xl p-5 shadow-aura">
              <p className="eyebrow">Streaming</p>
              <p className="mt-3 text-sm text-shell-muted">
                Follow persona progress in real time through the authenticated proxy.
              </p>
            </div>
          </div>
        </section>

        <section className="glass-panel rounded-[2rem] p-6 shadow-aura sm:p-8">
          <div className="mb-8 flex items-center justify-between">
            <div>
              <p className="eyebrow">Access</p>
              <h2 className="mt-2 font-display text-3xl font-semibold text-ink">Control Center Login</h2>
            </div>
            <div className="rounded-full border border-shell-border/40 bg-shell-panel/85 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">
              OIDC + MFA
            </div>
          </div>

          {error ? (
            <div className="mb-5 rounded-2xl border border-shell-danger/30 bg-shell-danger/12 px-4 py-3 text-sm text-shell-danger-text">
              {error === "mfa_required"
                ? "Your session is missing Authentik MFA verification. Sign in again to complete the protected flow."
                : "Authentication failed. Check the Authentik client settings and try again."}
            </div>
          ) : null}

          <div className="space-y-5">
            <SignInButton />
            <div className="rounded-2xl border border-shell-border/40 bg-shell-panel/85 p-4 text-sm text-shell-muted">
              <p className="font-semibold text-ink">Expected deployment shape</p>
              <p className="mt-2">
                Authentik handles identity. The UI then proxies every go-orca request server-side so the API stays off the public edge.
              </p>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}