import { NextRequest, NextResponse } from 'next/server';
import {
    ACCESS_COOKIE_MAX_AGE,
    REFRESH_COOKIE_MAX_AGE,
    clientIp,
    proxyError,
    rejectCrossSite,
    sessionCookie,
    uiCookie,
} from '../_lib/security';

const getAccountTypeFromToken = (token: string): string | null => {
    try {
        const payloadPart = token.split('.')[1];
        const decoded = Buffer.from(payloadPart, 'base64').toString('utf-8');
        const payload = JSON.parse(decoded);
        return payload?.account_type || null;
    } catch (e) {
        return null;
    }
};

const ALLOWED_METHODS = new Set(['GET', 'POST', 'PUT', 'PATCH', 'DELETE']);

/**
 * SECURITY: the endpoint comes from the browser, so it must stay a plain API
 * path under the backend base URL. Rejects path traversal ("..", including the
 * percent-encoded forms the URL parser also resolves), backslashes, scheme or
 * authority tricks, and the non-API surfaces (webhooks, swagger).
 */
function isSafeEndpoint(endpoint: unknown): endpoint is string {
    if (typeof endpoint !== 'string' || endpoint.length === 0 || endpoint.length > 4096) return false;
    if (endpoint.includes('#') || endpoint.includes('\\')) return false;

    // Only the PATH can redirect the request elsewhere on the backend; the query
    // string may legitimately carry encoded "/" or "." (e.g. ?object=kyc%2F…).
    const qIndex = endpoint.indexOf('?');
    const pathPart = (qIndex === -1 ? endpoint : endpoint.slice(0, qIndex)).toLowerCase();
    const queryPart = qIndex === -1 ? '' : endpoint.slice(qIndex + 1);

    if (
        pathPart.includes('..') ||
        pathPart.includes('%') || // no encoded traversal (%2e, %2f, %5c…) in the path
        pathPart.includes('//') ||
        pathPart.includes(':') ||
        pathPart.includes('@')
    ) {
        return false;
    }
    if (!/^\/?[a-z0-9\-._~/]*$/.test(pathPart)) return false;
    if (!/^[A-Za-z0-9\-._~%+=&,:@/!*'()$;]*$/.test(queryPart)) return false;

    const path = pathPart.replace(/^\/+/, '').replace(/^api\/v1\//, '');
    if (path.startsWith('webhooks') || path.startsWith('swagger')) return false;
    return true;
}

// In-flight refreshes, keyed by refresh token. SECURITY: this used to be a
// single module-level promise shared by ALL users — two users refreshing at the
// same time could receive each other's tokens. Keying by the refresh token
// keeps each user's refresh isolated while still de-duplicating parallel
// requests from the same browser.
const activeRefreshes = new Map<string, Promise<{ accessToken: string; refreshToken: string } | null>>();

async function performTokenRefresh(backendUrl: string, refreshToken: string, ip: string | null) {
    const inFlight = activeRefreshes.get(refreshToken);
    if (inFlight) {
        return inFlight;
    }

    const promise = (async () => {
        try {
            const headers: Record<string, string> = { 'Content-Type': 'application/json' };
            if (ip) headers['X-Forwarded-For'] = ip;
            const refreshRes = await fetch(`${backendUrl}/auth/token/refresh`, {
                method: 'POST',
                headers,
                body: JSON.stringify({
                    refresh_token: refreshToken,
                    refreshToken: refreshToken,
                }),
            });

            const refreshText = await refreshRes.text();
            let refreshData: any;
            try {
                refreshData = JSON.parse(refreshText);
            } catch (e) {
                refreshData = null;
            }

            if (refreshRes.status >= 200 && refreshRes.status < 300 && refreshData) {
                const dataObj = refreshData?.data || refreshData;
                const newAccessToken =
                    dataObj?.access_token ||
                    dataObj?.accessToken ||
                    dataObj?.token ||
                    refreshData?.access_token ||
                    refreshData?.accessToken ||
                    refreshData?.token ||
                    null;

                const newRefreshToken =
                    dataObj?.refresh_token ||
                    dataObj?.refreshToken ||
                    refreshData?.refresh_token ||
                    refreshData?.refreshToken ||
                    refreshToken;

                if (newAccessToken) {
                    return { accessToken: newAccessToken, refreshToken: newRefreshToken };
                }
            }
            return null;
        } catch (err) {
            console.error('[API PROXY] Token refresh failure');
            return null;
        } finally {
            setTimeout(() => {
                activeRefreshes.delete(refreshToken);
            }, 100);
        }
    })();

    activeRefreshes.set(refreshToken, promise);
    return promise;
}

function setSessionCookies(res: NextResponse, accessToken: string | null, refreshToken: string | null) {
    if (accessToken) {
        res.cookies.set('noe_token', accessToken, sessionCookie(ACCESS_COOKIE_MAX_AGE));
        const accType = getAccountTypeFromToken(accessToken);
        if (accType) {
            res.cookies.set('account_type', accType, uiCookie(REFRESH_COOKIE_MAX_AGE));
        }
    }
    if (refreshToken) {
        res.cookies.set('noe_refresh_token', refreshToken, sessionCookie(REFRESH_COOKIE_MAX_AGE));
    }
}

export async function POST(request: NextRequest) {
    const backendUrl = process.env.BACKEND_API_URL;
    if (!backendUrl) {
        console.error('[API PROXY] BACKEND_API_URL is not configured');
        return proxyError(500, 'PROXY_NOT_CONFIGURED', 'the API proxy is not configured');
    }

    // SECURITY (CSRF): only our own pages may use the session cookie through this proxy.
    const crossSite = rejectCrossSite(request);
    if (crossSite) return crossSite;

    // SECURITY (CSRF): a cross-site HTML form cannot send application/json
    // without a CORS preflight, which this route never grants.
    const contentType = request.headers.get('content-type') || '';
    if (!contentType.toLowerCase().startsWith('application/json')) {
        return proxyError(415, 'UNSUPPORTED_MEDIA_TYPE', 'Content-Type must be application/json');
    }

    try {
        let body: any;
        try {
            body = await request.json();
        } catch {
            return proxyError(400, 'INVALID_JSON', 'invalid JSON body');
        }
        const { endpoint, method = 'GET', body: reqBody } = body || {};

        if (!isSafeEndpoint(endpoint)) {
            return proxyError(400, 'INVALID_ENDPOINT', 'invalid or forbidden endpoint');
        }
        const upperMethod = String(method).toUpperCase();
        if (!ALLOWED_METHODS.has(upperMethod)) {
            return proxyError(405, 'METHOD_NOT_ALLOWED', 'method not allowed');
        }

        // Clean slash combinations
        let cleanedEndpoint = endpoint.startsWith('/') ? endpoint.slice(1) : endpoint;
        if (backendUrl.endsWith('/api/v1') && cleanedEndpoint.startsWith('api/v1/')) {
            cleanedEndpoint = cleanedEndpoint.slice(7);
        }
        const targetUrl = `${backendUrl}/${cleanedEndpoint}`;

        // SECURITY: headers sent by the browser are NOT forwarded (they could
        // override Authorization or spoof X-Forwarded-For). Only what the proxy
        // controls goes to the backend.
        const forwardHeaders = new Headers();
        forwardHeaders.set('Content-Type', 'application/json');
        const acceptLanguage = request.headers.get('accept-language');
        if (acceptLanguage) forwardHeaders.set('Accept-Language', acceptLanguage);
        const ip = clientIp(request);
        if (ip) forwardHeaders.set('X-Forwarded-For', ip);

        const cookieToken = request.cookies.get('noe_token')?.value;
        if (cookieToken) {
            forwardHeaders.set('Authorization', `Bearer ${cookieToken}`);
        }

        const fetchBody = reqBody && upperMethod !== 'GET' ? JSON.stringify(reqBody) : undefined;

        let response = await fetch(targetUrl, {
            method: upperMethod,
            headers: forwardHeaders,
            body: fetchBody,
        });

        const isAuthEndpoint =
            endpoint.includes('/auth/') ||
            endpoint.includes('/login') ||
            endpoint.includes('/register');
        let newAccessToken: string | null = null;
        let newRefreshToken: string | null = null;
        let didRefresh = false;

        // Perform token refresh rotation if 401 occurs on non-login/register endpoints
        if (response.status === 401 && !isAuthEndpoint) {
            const refreshToken = request.cookies.get('noe_refresh_token')?.value;

            if (refreshToken) {
                const refreshed = await performTokenRefresh(backendUrl, refreshToken, ip);
                if (refreshed && refreshed.accessToken) {
                    newAccessToken = refreshed.accessToken;
                    newRefreshToken = refreshed.refreshToken;
                    didRefresh = true;

                    forwardHeaders.set('Authorization', `Bearer ${newAccessToken}`);
                    response = await fetch(targetUrl, {
                        method: upperMethod,
                        headers: forwardHeaders,
                        body: fetchBody,
                    });
                }
            }
        }

        const resBodyText = await response.text();
        let parsedData: any;
        try {
            parsedData = JSON.parse(resBodyText);
        } catch (e) {
            parsedData = resBodyText;
        }

        const res = NextResponse.json(parsedData, { status: response.status });

        const dataObj = parsedData?.data || parsedData;
        const respAccessToken = isAuthEndpoint ? (dataObj?.access_token || dataObj?.accessToken || dataObj?.token) : null;
        const respRefreshToken = isAuthEndpoint ? (dataObj?.refresh_token || dataObj?.refreshToken) : null;

        // Save rotated access and refresh tokens to cookies
        if (didRefresh && newAccessToken && newRefreshToken) {
            setSessionCookies(res, newAccessToken, newRefreshToken);
        } else if (response.status >= 200 && response.status < 300 && isAuthEndpoint) {
            setSessionCookies(res, respAccessToken || null, respRefreshToken || null);
        }

        // If the server explicitly rejected auth (and refresh failed), or user logged out, wipe the cookies
        if ((response.status === 401 && !isAuthEndpoint) || endpoint.includes('/auth/logout')) {
            res.cookies.delete('noe_token');
            res.cookies.delete('noe_refresh_token');
            res.cookies.delete('account_type');
        }

        return res;
    } catch (error: any) {
        // SECURITY: log details server-side only; the browser gets a generic error.
        console.error('API Proxy POST handler error:', error);
        return proxyError(502, 'PROXY_ERROR', 'the service is temporarily unavailable');
    }
}
