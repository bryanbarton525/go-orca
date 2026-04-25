import { NextRequest, NextResponse } from "next/server";
import { auth } from "../../../../lib/auth/auth";
import { getServerEnv } from "../../../../lib/config/env";

export const dynamic = "force-dynamic";

const allowedRoutes = {
  GET: [
    /^healthz$/,
    /^readyz$/,
    /^workflows$/,
    /^workflows\/[^/]+$/,
    /^workflows\/[^/]+\/events$/,
    /^workflows\/[^/]+\/stream$/,
    /^workflows\/[^/]+\/attachments$/,
    /^providers$/,
    /^providers\/[^/]+\/models$/,
    /^scopes\/[^/]+\/effective-config$/,
    /^tenants$/,
    /^tenants\/[^/]+$/,
    /^tenants\/[^/]+\/scopes$/,
    /^customizations\/resolve$/,
    /^upload-sessions\/[^/]+$/,
    /^upload-sessions\/[^/]+\/files$/,
    /^mcp\/registry$/,
  ],
  POST: [
    /^workflows$/,
    /^workflows\/[^/]+\/cancel$/,
    /^workflows\/[^/]+\/resume$/,
    /^providers\/[^/]+\/test$/,
    /^tenants$/,
    /^tenants\/[^/]+\/scopes$/,
    /^upload-sessions$/,
    /^upload-sessions\/[^/]+\/files$/,
  ],
  PATCH: [/^tenants\/[^/]+$/, /^tenants\/[^/]+\/scopes\/[^/]+$/],
  DELETE: [/^tenants\/[^/]+$/, /^tenants\/[^/]+\/scopes\/[^/]+$/],
} as const;

function isAllowed(method: string, path: string) {
  const patterns = allowedRoutes[method as keyof typeof allowedRoutes] ?? [];
  return patterns.some((pattern) => pattern.test(path));
}

async function proxy(request: NextRequest, path: string[]) {
  const session = await auth();
  if (!session) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const joinedPath = path.join("/");
  if (!isAllowed(request.method, joinedPath)) {
    return NextResponse.json({ error: "Unsupported go-orca route" }, { status: 404 });
  }

  let baseUrl: string;
  try {
    baseUrl = getServerEnv().GO_ORCA_API_BASE_URL;
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "Missing server configuration" },
      { status: 500 }
    );
  }

  const upstreamUrl = new URL(`${baseUrl.replace(/\/$/, "")}/api/v1/${joinedPath}`);
  request.nextUrl.searchParams.forEach((value, key) => {
    if (key !== "tenantId" && key !== "scopeId") {
      upstreamUrl.searchParams.append(key, value);
    }
  });

  const upstreamHeaders = new Headers();
  const accept = request.headers.get("accept");
  const contentType = request.headers.get("content-type");
  const tenantId = request.nextUrl.searchParams.get("tenantId");
  const scopeId = request.nextUrl.searchParams.get("scopeId");

  if (accept) {
    upstreamHeaders.set("accept", accept);
  }
  if (contentType) {
    upstreamHeaders.set("content-type", contentType);
  }
  if (tenantId) {
    upstreamHeaders.set("X-Tenant-ID", tenantId);
  }
  if (scopeId) {
    upstreamHeaders.set("X-Scope-ID", scopeId);
  }

  const body =
    request.method === "GET" || request.method === "DELETE"
      ? undefined
      : contentType?.includes("multipart/form-data")
        ? await request.arrayBuffer()
        : await request.text();

  try {
    const upstreamResponse = await fetch(upstreamUrl, {
      method: request.method,
      cache: "no-store",
      headers: upstreamHeaders,
      body: body && (typeof body === "string" ? body.length > 0 : body.byteLength > 0) ? body : undefined,
      redirect: "manual",
    });

    const responseHeaders = new Headers();
    const upstreamContentType = upstreamResponse.headers.get("content-type");
    if (upstreamContentType) {
      responseHeaders.set("content-type", upstreamContentType);
    }
    responseHeaders.set("cache-control", upstreamResponse.headers.get("cache-control") ?? "no-store");

    if (upstreamContentType?.includes("text/event-stream")) {
      responseHeaders.set("connection", "keep-alive");
      responseHeaders.set("x-accel-buffering", "no");
      return new Response(upstreamResponse.body, {
        status: upstreamResponse.status,
        headers: responseHeaders,
      });
    }

    const text = await upstreamResponse.text();
    return new Response(text, {
      status: upstreamResponse.status,
      headers: responseHeaders,
    });
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "Failed to reach go-orca upstream" },
      { status: 502 }
    );
  }
}

export async function GET(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path);
}

export async function POST(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path);
}

export async function PATCH(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path);
}

export async function DELETE(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path);
}