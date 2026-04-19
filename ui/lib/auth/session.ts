import { redirect } from "next/navigation";
import type { Session } from "next-auth";
import { auth } from "./auth";

export type OrcaSession = Session & {
  mfaVerified?: boolean;
};

export async function getSession(): Promise<OrcaSession | null> {
  return (await auth()) as OrcaSession | null;
}

export function isMfaVerified(session: OrcaSession | null): boolean {
  if (!session) {
    return false;
  }

  if (process.env.NODE_ENV === "development" && session.mfaVerified === undefined) {
    return true;
  }

  return session.mfaVerified === true;
}

export async function requireSession(): Promise<OrcaSession> {
  const session = await getSession();

  if (!session) {
    redirect("/login");
  }

  if (!isMfaVerified(session)) {
    redirect("/login?error=mfa_required");
  }

  return session;
}