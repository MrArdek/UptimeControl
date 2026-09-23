const configuredBasePath = document.querySelector<HTMLMetaElement>('meta[name="uptime-base-path"]')?.content ?? ''
export const basePath = configuredBasePath === '__BASE_PATH__' ? '' : configuredBasePath

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message)
  }
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(`${basePath}/api/v1${path}`, {
    ...options,
    credentials: 'same-origin',
    headers: options.body
      ? { 'Content-Type': 'application/json', ...options.headers }
      : options.headers,
  })
  const isJSON = response.headers.get('content-type')?.includes('application/json')
  const body = isJSON ? await response.json() : null
  if (!response.ok) {
    throw new APIError(response.status, body?.error?.code ?? 'request_failed', body?.error?.message ?? `HTTP ${response.status}`)
  }
  return body as T
}

export function mutate<T>(path: string, method: string, body?: unknown, headers?: HeadersInit) {
  return api<T>(path, { method, body: body === undefined ? undefined : JSON.stringify(body), headers })
}
