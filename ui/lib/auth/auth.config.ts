import type { NextAuthConfig } from "next-auth";

function isPublicPath(pathname: string) {
  return (
    pathname === "/login" ||
    pathname === "/api/health" ||
    pathname.startsWith("/api/auth") ||
    pathname.startsWith("/_next") ||
    pathname.startsWith("/icons/") ||
    pathname === "/favicon.ico"
  );
}

export const authConfig: NextAuthConfig = {
  providers: [],
  trustHost: true,
  pages: {
    signIn: "/login",
    error: "/login",
  },
  callbacks: {
    authorized({ auth, request }) {
      if (isPublicPath(request.nextUrl.pathname)) {
        return true;
      }

      return !!auth?.user;
    },
    jwt({ token, account, profile }) {
      if (account) {
        token.accessToken = account.access_token;
        token.idToken = account.id_token;
      }

      if (profile && typeof profile === "object") {
        const candidate = profile as Record<string, unknown>;
        token.subject = typeof candidate.sub === "string" ? candidate.sub : token.subject;
        const mfaClaim = candidate.mfa_authenticated;
        token.mfaVerified = mfaClaim === undefined ? true : mfaClaim === true;
      }

      return token;
    },
    session({ session, token }) {
      session.accessToken = typeof token.accessToken === "string" ? token.accessToken : undefined;
      session.idToken = typeof token.idToken === "string" ? token.idToken : undefined;
      session.mfaVerified = typeof token.mfaVerified === "boolean" ? token.mfaVerified : undefined;
      const sessionUser = session.user as typeof session.user & { id?: string };
      if (typeof token.subject === "string") {
        sessionUser.id = token.subject;
      }
      return session;
    },
  },
  session: {
    strategy: "jwt",
    maxAge: 8 * 60 * 60,
  },
};