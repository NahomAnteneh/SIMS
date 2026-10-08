import { NextRequest, NextResponse } from "next/server";

export const dynamic = "force-dynamic";

function getTargetBase(): string {
  return (
    process.env.INTERNAL_QUERY_API_URL ||
    process.env.QUERY_API_URL ||
    "http://localhost:8080"
  );
}

async function handleProxy(
  request: NextRequest,
  context: { params: Promise<{ path?: string[] }> }
) {
  const { path } = await context.params;
  const targetBase = getTargetBase();
  const subpath = path && path.length > 0 ? path.join("/") : "";
  const search = request.nextUrl.search || "";
  const targetUrl = `${targetBase}/v1/${subpath}${search}`;

  const forwardHeaders = new Headers();
  request.headers.forEach((value, key) => {
    const lowerKey = key.toLowerCase();
    if (
      lowerKey !== "host" &&
      lowerKey !== "connection" &&
      lowerKey !== "content-length" &&
      lowerKey !== "transfer-encoding"
    ) {
      forwardHeaders.set(key, value);
    }
  });

  const method = request.method;
  const canHaveBody =
    method !== "GET" && method !== "HEAD" && method !== "OPTIONS";

  let body: ArrayBuffer | undefined = undefined;
  if (canHaveBody) {
    try {
      const buffer = await request.arrayBuffer();
      if (buffer.byteLength > 0) {
        body = buffer;
      }
    } catch {
      // Body empty or unreadable
    }
  }

  try {
    const upstreamRes = await fetch(targetUrl, {
      method,
      headers: forwardHeaders,
      body,
      redirect: "manual",
      // @ts-expect-error Node.js fetch duplex option for streaming
      duplex: "half",
    });

    const responseHeaders = new Headers();
    upstreamRes.headers.forEach((value, key) => {
      const lower = key.toLowerCase();
      if (
        lower !== "transfer-encoding" &&
        lower !== "connection" &&
        lower !== "content-encoding"
      ) {
        responseHeaders.set(key, value);
      }
    });

    return new NextResponse(upstreamRes.body, {
      status: upstreamRes.status,
      statusText: upstreamRes.statusText,
      headers: responseHeaders,
    });
  } catch (err: any) {
    return NextResponse.json(
      {
        type: "https://sims.internal/errors/bad-gateway",
        title: "Bad Gateway",
        status: 502,
        detail: `Failed to proxy request to backend at ${targetUrl}: ${err?.message || "Connection refused"}`,
      },
      { status: 502 }
    );
  }
}

export const GET = handleProxy;
export const POST = handleProxy;
export const PUT = handleProxy;
export const PATCH = handleProxy;
export const DELETE = handleProxy;
export const OPTIONS = handleProxy;
export const HEAD = handleProxy;
