import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export function middleware(request: NextRequest) {
  const token = request.cookies.get("sims_auth_token")?.value;
  const { pathname } = request.nextUrl;

  // Always allow Next.js internals, public assets, and API routes
  if (
    pathname.startsWith("/_next") ||
    pathname.startsWith("/api") ||
    pathname === "/favicon.ico"
  ) {
    return NextResponse.next();
  }

  // If already authenticated and visiting /login, redirect to /compute/vms
  if (pathname === "/login") {
    if (token) {
      return NextResponse.redirect(new URL("/compute/vms", request.url));
    }
    return NextResponse.next();
  }

  // If not authenticated and visiting protected route, redirect to /login
  if (!token) {
    const loginUrl = new URL("/login", request.url);
    return NextResponse.redirect(loginUrl);
  }

  // If visiting / or /dashboard, redirect to /compute/vms
  if (pathname === "/" || pathname === "/dashboard") {
    return NextResponse.redirect(new URL("/compute/vms", request.url));
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    /*
     * Match all request paths except for the ones starting with:
     * - _next/static (static files)
     * - _next/image (image optimization files)
     * - favicon.ico (favicon file)
     */
    "/((?!_next/static|_next/image|favicon.ico).*)",
  ],
};
