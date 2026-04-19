"use client";

import { signOut } from "next-auth/react";
import { useState } from "react";

export function LogoutButton({ callbackUrl = "/login" }: { callbackUrl?: string }) {
  const [loading, setLoading] = useState(false);

  async function handleClick() {
    setLoading(true);
    await signOut({ callbackUrl });
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={loading}
      className="rounded-full border border-shell-border/45 bg-shell-panel/80 px-4 py-2 text-sm font-medium text-ink transition hover:border-lagoon hover:text-lagoon disabled:cursor-not-allowed disabled:opacity-70"
    >
      {loading ? "Signing out..." : "Sign out"}
    </button>
  );
}