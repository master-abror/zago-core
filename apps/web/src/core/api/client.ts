// core/api — SATU-SATUNYA tempat fetch dilakukan. URL selalu relatif (same-origin).
// Komponen dan modul tidak boleh memanggil fetch() langsung (CLAUDE.md).

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export async function apiGet<T>(path: string, fetchImpl: FetchLike = fetch): Promise<T> {
  if (!path.startsWith('/')) {
    throw new Error('path API harus relatif terhadap origin dan diawali "/"');
  }
  const res = await fetchImpl(path, { headers: { Accept: 'application/json' } });
  if (!res.ok) {
    throw new ApiError(res.status, `permintaan ${path} gagal dengan status ${res.status}`);
  }
  return (await res.json()) as T;
}
