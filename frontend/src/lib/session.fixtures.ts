// Synthetic session answers for tests. The tokens are sentinels: tests search
// browser storage, cookies and the address for them.
import type { SessionInfo } from './session';

export const sentinelCsrf = 'csrf-sentinel-7c1e9a';

export const signedIn: SessionInfo = {
  schema_version: 1,
  state: 'authenticated',
  username: 'admin-test',
  csrf_token: sentinelCsrf,
  expires_at: '2026-10-29T00:00:00Z',
  idle_expires_at: '2026-10-28T13:00:00Z',
  idle_timeout_seconds: 3600,
};

export const loginRequired: SessionInfo = { schema_version: 1, state: 'login_required' };
export const setupRequired: SessionInfo = { schema_version: 1, state: 'setup_required' };
