// Minimal fetch wrapper for gemba server endpoints (gm-xgm / M1.6).
//
// The gemba server wraps every handler failure in the httperr envelope
// (internal/server/httperr): `{"error": "<code>", "message": "..."}`.
// ApiError preserves both parts plus the HTTP status so callers can
// branch on typed kind — `adaptor_degraded` (503), `session_not_found`
// (404), `adaptor_not_configured` (503), `validation` (400) — without
// parsing the human-readable message (see core.gen.ts AdaptorErrorKind
// and the contract in gm-faz).

export const API_BASE = '/api';
export const ADAPTOR_OPERATION_FAILED_EVENT = 'gemba:adaptor-operation-failed';

export interface AdaptorOperationFailedDetail {
  status: number;
  code: string;
  message: string;
  url: string;
}

// ApiError is thrown by apiFetch for any non-2xx response. The `code`
// field is the envelope's `error` token when one was present, or a
// well-known synthetic token for transport-level failures (network,
// non-JSON body). Use `code` + `status` for branching; reserve
// `message` for display.
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }

  // True when the adaptor-boundary reported itself degraded and the
  // caller should surface a banner + keep previously loaded data
  // visible (gm-faz retryable = true for this kind). Covers both
  // "adaptor not wired up yet" and "wired up but unhealthy".
  get isAdaptorDegraded(): boolean {
    return (
      this.status === 503 &&
      (this.code === 'adaptor_degraded' || this.code === 'adaptor_not_configured')
    );
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }
}

export interface ApiFetchInit extends RequestInit {
  // Override the base URL. Defaults to API_BASE. Tests use this to
  // point the client at a mock server; resolveTransport() may supply a
  // transport-specific base in a future milestone.
  base?: string;
}

// apiFetch performs a JSON fetch against the gemba server and returns
// the decoded body on success. On failure it throws an ApiError that
// preserves the envelope's code so callers can branch typedly.
//
// Transport-level failures (network unreachable, JSON parse, non-JSON
// body) become ApiError with synthetic codes:
//   - "network"     — fetch rejected (offline, CORS, DNS)
//   - "parse"       — 2xx body was not valid JSON
//   - "bad_envelope"— non-2xx body was not the {error,message} envelope
//                     (bare string, HTML error page, etc.)
export async function apiFetch<T>(path: string, init: ApiFetchInit = {}): Promise<T> {
  if (!path.startsWith('/')) {
    throw new Error(`apiFetch: path must start with '/'; got ${path}`);
  }
  const { base = API_BASE, headers, ...rest } = init;
  const url = `${base}${path}`;

  let res: Response;
  try {
    res = await fetch(url, {
      ...rest,
      headers: {
        Accept: 'application/json',
        ...headers,
      },
    });
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    throw new ApiError(0, 'network', `fetch ${url}: ${msg}`);
  }

  if (!res.ok) {
    // Try to decode the shared envelope. If the server emitted HTML or
    // a bare string (legacy paths, reverse proxy errors), synthesize a
    // "bad_envelope" ApiError so callers still see typed failures.
    let code = 'bad_envelope';
    let message = `${res.status} ${res.statusText}`;
    try {
      const body: unknown = await res.json();
      if (isEnvelope(body)) {
        code = body.error;
        message = body.message;
      }
    } catch {
      // fall through to bad_envelope
    }
    if (isAdaptorFailure(res.status, code)) {
      notifyAdaptorOperationFailed({ status: res.status, code, message, url });
    }
    throw new ApiError(res.status, code, message);
  }

  try {
    return (await res.json()) as T;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    throw new ApiError(res.status, 'parse', `decode ${url}: ${msg}`);
  }
}

function isAdaptorFailure(status: number, code: string): boolean {
  return (
    status === 503 &&
    (code === 'adaptor_degraded' || code === 'adaptor_not_configured')
  );
}

function notifyAdaptorOperationFailed(detail: AdaptorOperationFailedDetail): void {
  if (typeof window === 'undefined' || typeof window.dispatchEvent !== 'function') {
    return;
  }
  window.dispatchEvent(new CustomEvent(ADAPTOR_OPERATION_FAILED_EVENT, { detail }));
}

interface Envelope {
  error: string;
  message: string;
}

function isEnvelope(v: unknown): v is Envelope {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as { error?: unknown }).error === 'string' &&
    typeof (v as { message?: unknown }).message === 'string'
  );
}
