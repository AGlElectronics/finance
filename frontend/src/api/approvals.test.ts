import { describe, expect, it } from 'vitest';

import { describeApproval, isPending, samePerson, type Approval } from './approvals';
import { can, type Me } from './me';

const approval = (over: Partial<Approval>): Approval => ({
  id: 'a1',
  action: 'payment.record',
  target_id: 'inv-1',
  target_number: 'FIN-0003',
  amount_cents: 12100,
  payload: { paid_on: '2026-11-05', reference: 'BANK-77' },
  status: 'pending',
  requested_by: { name: 'bea', issuer: 'https://idp', subject: 'sub-bea' },
  requested_at: '2026-11-05T10:00:00Z',
  decided_by: null,
  decided_at: null,
  decision_note: '',
  result_id: null,
  ...over,
});

describe('approvals', () => {
  it('recognises a 202 pending answer', () => {
    expect(isPending({ status: 'pending_approval', approval: approval({}) })).toBe(true);
    expect(isPending({ id: 'inv', status: 'paid' })).toBe(false);
    expect(isPending(null)).toBe(false);
  });

  it('compares people by issuer and subject, else by name', () => {
    const a = approval({}).requested_by;
    expect(samePerson(a, { username: 'someone else', issuer: 'https://idp', subject: 'sub-bea' })).toBe(true);
    expect(samePerson(a, { username: 'bea', issuer: 'https://idp', subject: 'sub-other' })).toBe(false);
    expect(samePerson({ name: 'bea', issuer: '', subject: '' }, { username: 'bea' })).toBe(true);
  });

  it('describes each kind of request', () => {
    expect(describeApproval(approval({}))).toBe('Payment for FIN-0003 on 2026-11-05 (ref BANK-77)');
    expect(describeApproval(approval({ action: 'invoice.void', payload: { reason: 'sent twice' } }))).toBe('Void FIN-0003: sent twice');
    expect(describeApproval(approval({ action: 'expense.create', target_id: null, payload: { vendor: 'Printer', occurred_on: '2026-11-01' } })))
      .toBe('Expense to Printer on 2026-11-01');
    expect(describeApproval(approval({ action: 'credit_note.issue', payload: { reason: 'cancelled', full: true } })))
      .toBe('Credit note for FIN-0003 (whole invoice): cancelled');
  });
});

describe('can', () => {
  const me = (permissions: Me['permissions']): Me => ({
    username: 'u', auth_mode: 'oidc', has_access: true, groups: [], roles: [], permissions,
  });
  it('checks the permissions the API sent', () => {
    expect(can(me(['finance.read', 'finance.write']), 'finance.write')).toBe(true);
    expect(can(me(['finance.read']), 'finance.approve')).toBe(false);
    expect(can(undefined, 'finance.read')).toBe(false);
  });
});
