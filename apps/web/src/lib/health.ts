import { ApiError, apiGet } from '../core/api/client';

export type ReadyState = 'checking' | 'ready' | 'unavailable';

export async function fetchReadyState(): Promise<ReadyState> {
  try {
    await apiGet('/health/ready');
    return 'ready';
  } catch (e) {
    if (e instanceof ApiError || e instanceof TypeError) {
      return 'unavailable';
    }
    throw e;
  }
}
