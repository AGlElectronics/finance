import { describe, expect, it } from 'vitest';

import { isDenCredentialRevoked, isDenPairingRequired } from './den';

describe('den state', () => {
  it('requires pairing only in required mode without a link', () => {
    expect(isDenPairingRequired({ mode: 'required', paired: false })).toBe(true);
    expect(isDenPairingRequired({ mode: 'required', paired: true, revoked: true })).toBe(false);
    expect(isDenPairingRequired({ mode: 'standalone', paired: false })).toBe(false);
    expect(isDenPairingRequired(undefined)).toBe(false);
  });

  it('reports a revoked credential only for a paired module', () => {
    expect(isDenCredentialRevoked({ paired: true, revoked: true })).toBe(true);
    expect(isDenCredentialRevoked({ paired: true, revoked: false })).toBe(false);
    expect(isDenCredentialRevoked({ paired: true })).toBe(false);
    expect(isDenCredentialRevoked({ paired: false, revoked: true })).toBe(false);
    expect(isDenCredentialRevoked(undefined)).toBe(false);
  });
});
