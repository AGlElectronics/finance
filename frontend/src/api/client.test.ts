import { afterEach, describe, expect, it, vi } from 'vitest';

import { newIdempotencyKey } from './client';
import { createExpense } from './expenses';
import { addPayment } from './invoices';

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(status: number, body: unknown) {
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } }));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('idempotency keys', () => {
  it('are unique per attempt', () => {
    expect(newIdempotencyKey()).not.toBe(newIdempotencyKey());
  });

  it('are sent on payments and reused when the caller retries with the same key', async () => {
    const fetchMock = stubFetch(201, { id: 'inv-1' });
    await addPayment('inv-1', { amount_cents: 100, method: 'cash', reference: '', note: '' }, 'key-1');
    await addPayment('inv-1', { amount_cents: 100, method: 'cash', reference: '', note: '' }, 'key-1');
    const keys = fetchMock.mock.calls.map(([, init]) => (init?.headers as Record<string, string>)['Idempotency-Key']);
    expect(keys).toEqual(['key-1', 'key-1']);
    expect(fetchMock.mock.calls[0][0]).toContain('/api/invoices/inv-1/payments');
  });

  it('get a fresh key by default and pass a 202 pending answer through', async () => {
    const fetchMock = stubFetch(202, { status: 'pending_approval', approval: { id: 'a1' } });
    const res = await createExpense({ vendor: 'Printer', amount_cents: 500 });
    const key = (fetchMock.mock.calls[0][1]?.headers as Record<string, string>)['Idempotency-Key'];
    expect(key).toMatch(/.{8,}/);
    expect(res).toEqual({ status: 'pending_approval', approval: { id: 'a1' } });
  });

  it('turn API errors into ApiError with the code', async () => {
    stubFetch(409, { error: { code: 'period_closed', message: '2026-10 is closed; an admin must reopen it first' } });
    await expect(createExpense({ vendor: 'x', amount_cents: 1 })).rejects.toMatchObject({ status: 409, code: 'period_closed' });
  });
});
