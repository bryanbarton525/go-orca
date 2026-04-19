# go-orca-ui

Authenticated control surface for go-orca.

## Environment

Create `.env.local` from the example below and supply Authentik plus upstream go-orca settings:

```env
OIDC_ISSUER_URL=https://auth.iambarton.com/application/o/go-orca-ui/
OIDC_CLIENT_ID=replace-me
OIDC_CLIENT_SECRET=replace-me
NEXTAUTH_SECRET=replace-with-at-least-32-random-characters
NEXTAUTH_URL=http://localhost:3000
GO_ORCA_API_BASE_URL=http://localhost:8080
NEXT_PUBLIC_ORCA_DEFAULT_TENANT_ID=
NEXT_PUBLIC_ORCA_DEFAULT_SCOPE_ID=
```

This Authentik deployment publishes an issuer with a trailing slash in its discovery document, so use that exact issuer value. Configure Authentik with these callback targets:

- Local callback: `http://localhost:3000/api/auth/callback/authentik`
- Production callback: `https://orca.iambarton.com/api/auth/callback/authentik`
- Local sign-in page: `http://localhost:3000/login`
- Production sign-in page: `https://orca.iambarton.com/login`

## Local Development

```bash
pnpm install
pnpm dev
```

The app signs users in with Authentik and proxies all go-orca API calls through `app/api/orca/[...path]`, so the raw upstream API does not need to be exposed to the browser.

## Container Image

The repo includes a multi-stage Dockerfile and a GitHub Actions workflow that publishes:

- `ghcr.io/bryanbarton525/go-orca-ui:latest`
- `ghcr.io/bryanbarton525/go-orca-ui:sha-<commit>`
