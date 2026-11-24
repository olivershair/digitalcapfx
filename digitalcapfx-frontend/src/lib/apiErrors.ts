/**
 * Central, translated error messages.
 *
 * The backend answers errors as `{ success: false, error: { code, message } }`
 * with an English `message`. That text must never reach the user as-is: this
 * module recognises the error (by exact message, message pattern, then code,
 * then HTTP status) and returns an explicit message in the language the user
 * selected. Use `apiErrorMessage(err)` in every `toast.error`.
 */
import i18n from '@/lib/i18n'
import { ENTRIES, STATUS_ENTRIES } from '@/lib/errorCatalog'

const normalize = (s: string) =>
  s.toLowerCase().replace(/\s+/g, ' ').trim().replace(/[.!]+$/, '')

/** Codes too generic to say anything on their own: the message or status decides. */
const GENERIC_CODES = new Set(['VALIDATION_ERROR', 'BAD_REQUEST', 'INVALID_TOKEN'])

interface Extracted {
  status?: number
  code?: string
  message?: string
  network?: boolean
  timeout?: boolean
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- inspects untyped Axios errors and envelopes
function extract(err: any): Extracted {
  if (!err) return {}
  if (typeof err === 'string') return { message: err }

  // Axios error with a response, or a resolved `{ success: false, error }` envelope.
  const data = err?.response?.data ?? (err?.error !== undefined || err?.success === false ? err : undefined)
  const status: number | undefined = err?.response?.status ?? err?.status
  const raw = data?.error

  if (raw && typeof raw === 'object') {
    return { status, code: raw.code, message: raw.message }
  }
  if (typeof raw === 'string') {
    return { status, message: raw }
  }
  if (typeof data?.message === 'string' && data?.success === false) {
    return { status, message: data.message }
  }
  if (err?.isAxiosError && !err?.response) {
    const timeout = err?.code === 'ECONNABORTED' || err?.code === 'ETIMEDOUT'
    return { network: !timeout, timeout }
  }
  return { status }
}

function lookup(ex: Extracted): { key: string; params?: Record<string, string> } | null {
  if (ex.timeout) return { key: 'errors.timeout' }
  if (ex.network) return { key: 'errors.network' }

  if (ex.message) {
    const msg = normalize(ex.message)
    for (const e of ENTRIES) {
      if (e.messages?.some((m) => normalize(m) === msg)) return { key: `errors.${e.key}` }
    }
    for (const e of ENTRIES) {
      if (e.pattern) {
        const m = msg.match(e.pattern)
        if (m) {
          const params: Record<string, string> = {}
          e.params?.forEach((name, i) => { params[name] = m[i + 1] })
          return { key: `errors.${e.key}`, params }
        }
      }
    }
    for (const e of ENTRIES) {
      if (e.prefixes?.some((p) => msg.startsWith(normalize(p)))) return { key: `errors.${e.key}` }
    }
  }

  if (ex.code && !GENERIC_CODES.has(ex.code)) {
    const e = ENTRIES.find((x) => x.codes?.includes(ex.code!))
    if (e) return { key: `errors.${e.key}` }
  }

  return null
}

/** Statuses whose generic text says less than "what failed" (the caller's fallback). */
const VAGUE_STATUSES = new Set([400, 404, 409, 422])

function statusKey(status?: number): string | null {
  if (!status) return null
  if (STATUS_ENTRIES[String(status)]) return `errors.status.${status}`
  return status >= 500 ? 'errors.status.500' : null
}

/**
 * Returns an explicit error message in the user's language.
 *
 * @param err      an Axios error, a `{ success: false, error }` envelope, or a string
 * @param fallback translation key (or already translated text) used when the
 *                 error is not recognised — describe what failed, e.g. 'errors.generic'
 */
export function apiErrorMessage(err: unknown, fallback = 'errors.generic'): string {
  const ex = extract(err)
  const hit = lookup(ex)
  if (hit) return i18n.t(hit.key, hit.params)

  // Unrecognised error. English UI: the backend text is in the right language
  // and is the most precise thing we have.
  const lang = (i18n.language || 'fr').slice(0, 2)
  if (lang === 'en' && ex.message) {
    return ex.message.charAt(0).toUpperCase() + ex.message.slice(1)
  }

  // Session, permission, rate-limit and server errors: the status says it best.
  // Otherwise "what failed" (the caller's fallback) is more explicit.
  const byStatus = statusKey(ex.status)
  const hasSpecificFallback = fallback !== 'errors.generic'
  if (byStatus && !(hasSpecificFallback && VAGUE_STATUSES.has(ex.status!))) {
    return i18n.t(byStatus)
  }
  return i18n.exists(fallback) ? i18n.t(fallback) : fallback
}
