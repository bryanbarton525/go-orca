import { z } from "zod";

const serverEnvSchema = z.object({
  GO_ORCA_API_BASE_URL: z.string().url("GO_ORCA_API_BASE_URL must be a valid URL"),
  NEXTAUTH_SECRET: z.string().min(32, "NEXTAUTH_SECRET must be at least 32 characters"),
  NEXTAUTH_URL: z.string().url("NEXTAUTH_URL must be a valid URL").optional(),
  OIDC_CLIENT_ID: z.string().min(1, "OIDC_CLIENT_ID is required"),
  OIDC_CLIENT_SECRET: z.string().min(1, "OIDC_CLIENT_SECRET is required"),
  OIDC_ISSUER_URL: z.string().url("OIDC_ISSUER_URL must be a valid URL"),
  NODE_ENV: z.enum(["development", "production", "test"]).default("development"),
});

export type ServerEnv = z.infer<typeof serverEnvSchema>;

let cachedServerEnv: ServerEnv | undefined;

export function getServerEnv(): ServerEnv {
  if (cachedServerEnv) {
    return cachedServerEnv;
  }

  const result = serverEnvSchema.safeParse(process.env);
  if (!result.success) {
    const details = result.error.issues
      .map((issue) => `  - ${issue.path.join(".")}: ${issue.message}`)
      .join("\n");
    throw new Error(`Invalid server environment:\n${details}`);
  }

  cachedServerEnv = result.data;
  return cachedServerEnv;
}

export function getWorkspaceDefaults() {
  return {
    tenantId: process.env.NEXT_PUBLIC_ORCA_DEFAULT_TENANT_ID ?? "",
    scopeId: process.env.NEXT_PUBLIC_ORCA_DEFAULT_SCOPE_ID ?? "",
  };
}