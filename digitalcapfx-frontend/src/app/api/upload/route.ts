import { NextRequest } from 'next/server';
import { NextResponse } from 'next/server';
import { clientIp, proxyError, rejectCrossSite } from '../_lib/security';

const ALLOWED_PURPOSES = new Set(['kyc', 'avatar', 'document', 'business', 'misc']);
const MAX_UPLOAD_BYTES = 15 * 1024 * 1024;

export async function POST(request: NextRequest) {
    const backendUrl = process.env.BACKEND_API_URL;
    if (!backendUrl) {
        console.error('[FILE PROXY] BACKEND_API_URL is not configured');
        return proxyError(500, 'PROXY_NOT_CONFIGURED', 'the upload service is not configured');
    }

    // SECURITY (CSRF): a multipart form can be posted cross-site without a
    // preflight, so the Origin check is the protection here.
    const crossSite = rejectCrossSite(request);
    if (crossSite) return crossSite;

    const declaredLength = Number(request.headers.get('content-length') || 0);
    if (declaredLength > MAX_UPLOAD_BYTES + 64 * 1024) {
        return proxyError(413, 'FILE_TOO_LARGE', 'file exceeds the 15 MB limit');
    }

    try {
        const formData = await request.formData();
        const file = formData.get('file');
        const purpose = String(formData.get('purpose') || 'misc').toLowerCase();

        if (!file || typeof file === 'string') {
            return proxyError(400, 'VALIDATION_ERROR', 'no file provided');
        }
        if (!ALLOWED_PURPOSES.has(purpose)) {
            return proxyError(400, 'VALIDATION_ERROR', 'invalid purpose');
        }

        // Reconstruct backend form data
        const forwardFormData = new FormData();
        forwardFormData.append('file', file);
        forwardFormData.append('purpose', purpose);

        // Prepare headers with Bearer token authorization
        const headers = new Headers();
        const cookieToken = request.cookies.get('noe_token')?.value;
        if (cookieToken) {
            headers.set('Authorization', `Bearer ${cookieToken}`);
        }
        const ip = clientIp(request);
        if (ip) headers.set('X-Forwarded-For', ip);

        const response = await fetch(`${backendUrl}/uploads`, {
            method: 'POST',
            headers,
            body: forwardFormData,
        });

        const resText = await response.text();
        let parsedData;
        try {
            parsedData = JSON.parse(resText);
        } catch (e) {
            parsedData = resText;
        }

        return NextResponse.json(parsedData, { status: response.status });
    } catch (error: any) {
        // SECURITY: details stay in server logs; the browser gets a generic error.
        console.error('File Upload Proxy handler error:', error);
        return proxyError(502, 'PROXY_ERROR', 'the upload service is temporarily unavailable');
    }
}
