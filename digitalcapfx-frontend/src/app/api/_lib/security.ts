import { NextRequest, NextResponse } from 'next/server';

/**
 * Security helpers shared by the server-side API routes (/api/proxy, /api/upload).
 *
 * These routes authenticate to the backend with the user's httpOnly session
 * cookie, so they are a CSRF target: any site could make the victim's browser
 * POST to them. Every request is therefore checked for:
 *   - a same-origin `Origin` header (or `Sec-Fetch-Site: same-origin`),
 *   - and, for the JSON proxy, a JSON content type (a cross-site HTML form can
 *     only send text/plain, urlencoded or multipart without a CORS preflight).
 */

const isProd = process.env.NODE_ENV === 'production';

/** Extra origins allowed to call the API routes (comma-separated), e.g. a staging domain. */
const extraOrigins = (process.env.ALLOWED_ORIGINS || '')
    .split(',')
    .map((o) => o.trim().replace(/\/$/, ''))
    .filter(Boolean);

function requestHost(request: NextRequest): string | null {
    return request.headers.get('x-forwarded-host') || request.headers.get('host');
}

/** Returns a 403 response when the request does not come from our own pages. */
export function rejectCrossSite(request: NextRequest): NextResponse | null {
    const origin = request.headers.get('origin');
    const fetchSite = request.headers.get('sec-fetch-site');

    if (origin) {
        let originHost: string;
        try {
            originHost = new URL(origin).host;
        } catch {
            return forbidden('invalid Origin header');
        }
        const host = requestHost(request);
        if ((host && originHost === host) || extraOrigins.includes(origin.replace(/\/$/, ''))) {
            return null;
        }
        return forbidden('cross-site request refused');
    }

    // No Origin header: only accept what the browser itself labels same-origin.
    if (fetchSite === 'same-origin') {
        return null;
    }
    return forbidden('cross-site request refused (missing Origin)');
}

function forbidden(reason: string) {
    return NextResponse.json(
        { success: false, error: { code: 'CSRF_REJECTED', message: reason } },
        { status: 403 },
    );
}

/**
 * The visitor's IP as seen by our own front end. On Cloud Run the Google
 * front end APPENDS the real client address, so the right-most
 * X-Forwarded-For entry is trustworthy; anything to its left is client-supplied.
 */
export function clientIp(request: NextRequest): string | null {
    const xff = request.headers.get('x-forwarded-for');
    if (!xff) return null;
    const parts = xff.split(',').map((p) => p.trim()).filter(Boolean);
    return parts.length ? parts[parts.length - 1] : null;
}

/** Access-token cookie lifetime — matches the backend JWT access expiry (30 min default). */
export const ACCESS_COOKIE_MAX_AGE = Number(process.env.ACCESS_COOKIE_MAX_AGE_SECONDS || 30 * 60);
/** Refresh-token cookie lifetime — matches the backend refresh expiry (7 days default). */
export const REFRESH_COOKIE_MAX_AGE = Number(process.env.REFRESH_COOKIE_MAX_AGE_SECONDS || 7 * 24 * 60 * 60);

/** Cookie options for the session cookies: httpOnly, Secure in production, SameSite=Lax. */
export function sessionCookie(maxAge: number) {
    return {
        httpOnly: true,
        secure: isProd,
        sameSite: 'lax' as const,
        path: '/',
        maxAge,
    };
}

/** Non-sensitive UI hint cookie (readable by JS). */
export function uiCookie(maxAge: number) {
    return { ...sessionCookie(maxAge), httpOnly: false };
}

/** Generic error body — never leak internal error messages to the browser. */
export function proxyError(status: number, code: string, message: string) {
    return NextResponse.json({ success: false, error: { code, message } }, { status });
}
