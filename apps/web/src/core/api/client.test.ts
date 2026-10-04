import { describe, expect, it } from 'vitest';
import { ApiError, apiGet, type FetchLike } from './client';

const jsonResponse = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

describe('apiGet', () => {
  it('mengembalikan JSON saat status 2xx', async () => {
    const fake: FetchLike = async () => jsonResponse(200, { status: 'ok' });
    await expect(apiGet<{ status: string }>('/health', fake)).resolves.toEqual({ status: 'ok' });
  });

  it('melempar ApiError dengan status saat respons non-2xx', async () => {
    const fake: FetchLike = async () => jsonResponse(503, { status: 'unavailable' });
    const err = await apiGet('/health/ready', fake).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(503);
  });

  it('menolak URL absolut (harus same-origin)', async () => {
    const fake: FetchLike = async () => jsonResponse(200, {});
    await expect(apiGet('https://evil.example/x', fake)).rejects.toThrow('relatif');
  });
});
