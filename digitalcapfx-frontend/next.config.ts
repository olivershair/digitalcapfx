import type { NextConfig } from "next";

const isDev = process.env.NODE_ENV !== "production";

/**
 * SECURITY headers applied to every page and API route.
 *
 * - frame-ancestors 'none' + X-Frame-Options DENY: the site cannot be framed,
 *   which blocks clickjacking (e.g. a hidden transfer/withdrawal form).
 * - CSP: scripts/styles/connections limited to our own origin plus the few
 *   third parties the app really uses (Fontshare fonts, Didit KYC iframe,
 *   Google Cloud Storage signed URLs for document previews).
 *   'unsafe-inline' is still needed for Next.js inline bootstrap scripts;
 *   moving to nonces is a later hardening step.
 * - HSTS, nosniff, strict referrer, and a restrictive Permissions-Policy
 *   (camera/microphone left at the browser default for the Didit liveness check).
 */
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline' https://api.fontshare.com",
  "font-src 'self' data: https://api.fontshare.com https://cdn.fontshare.com",
  "img-src 'self' data: blob: https:",
  `connect-src 'self'${isDev ? " ws: http://localhost:*" : ""}`,
  "frame-src 'self' blob: https://*.didit.me https://storage.googleapis.com",
  "media-src 'self' blob:",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
  ...(isDev ? [] : ["upgrade-insecure-requests"]),
].join("; ");

const securityHeaders = [
  { key: "Content-Security-Policy", value: csp },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains; preload" },
  {
    key: "Permissions-Policy",
    // camera/microphone are left at the browser default so the Didit iframe
    // (allow="camera; microphone") keeps working for the liveness check.
    value: "geolocation=(), payment=(), usb=(), serial=(), hid=(), bluetooth=()",
  },
];

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

export default nextConfig;
