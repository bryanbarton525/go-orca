import NextAuth from "next-auth";
import AuthentikProvider from "next-auth/providers/authentik";
import { authConfig } from "./auth.config";

export const { auth, handlers, signIn, signOut } = NextAuth({
  ...authConfig,
  providers: [
    AuthentikProvider({
      clientId: process.env.OIDC_CLIENT_ID!,
      clientSecret: process.env.OIDC_CLIENT_SECRET!,
      issuer: process.env.OIDC_ISSUER_URL!,
    }),
  ],
});