import NextAuth from "next-auth";
import type { Provider } from "next-auth/providers";
import AuthentikProvider from "next-auth/providers/authentik";
import { authConfig } from "./auth.config";

const providers: Provider[] = [];

if (process.env.OIDC_CLIENT_ID && process.env.OIDC_CLIENT_SECRET && process.env.OIDC_ISSUER_URL) {
  providers.push(
    AuthentikProvider({
      clientId: process.env.OIDC_CLIENT_ID,
      clientSecret: process.env.OIDC_CLIENT_SECRET,
      issuer: process.env.OIDC_ISSUER_URL,
    }),
  );
}

export const { auth, handlers, signIn, signOut } = NextAuth({
  ...authConfig,
  providers,
});