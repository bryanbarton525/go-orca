import NextAuth from "next-auth";
import { authConfig } from "./lib/auth/auth.config";

export const { auth: middleware } = NextAuth(authConfig);

export const config = {
  matcher: ["/((?!login|api/auth|api/health|_next/static|_next/image|favicon.ico|icons/).*)"],
};