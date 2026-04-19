"use client";

import { signIn } from "next-auth/react";
import { useState } from "react";

export function SignInButton({ callbackUrl = "/overview" }: { callbackUrl?: string }) {
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    try {
      await signIn("authentik", { callbackUrl });
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={loading}
      className="inline-flex w-full items-center justify-center rounded-2xl bg-lagoon px-5 py-3 text-sm font-semibold text-white transition hover:bg-lagoon-hover disabled:cursor-not-allowed disabled:opacity-70"
      aria-busy={loading}
    >
      {loading ? "Redirecting to Authentik..." : "Sign in with Authentik"}
    </button>
  );
}