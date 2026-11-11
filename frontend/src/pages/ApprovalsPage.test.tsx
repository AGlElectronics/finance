import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApprovalsPage } from './ApprovalsPage';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
}));

const person = (name: string) => ({ name, issuer: 'https://idp', subject: `sub-${name}` });

const approvals = [
  {
    id: 'a-theirs', action: 'payment.record', target_id: 'inv-1', target_number: 'FIN-0001', amount_cents: 12100,
    payload: { paid_on: '2026-11-05' }, status: 'pending', requested_by: person('bea'), requested_at: '2026-11-05T10:00:00Z',
    decided_by: null, decided_at: null, decision_note: '', result_id: null,
  },
  {
    id: 'a-mine', action: 'expense.create', target_id: null, amount_cents: 5000,
    payload: { vendor: 'Printer' }, status: 'pending', requested_by: person('aki'), requested_at: '2026-11-06T10:00:00Z',
    decided_by: null, decided_at: null, decision_note: '', result_id: null,
  },
];

function stubApi(permissions: string[]) {
  const calls: { url: string; method: string; body?: string }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const path = new URL(url).pathname;
    calls.push({ url: path, method: init?.method ?? 'GET', body: init?.body as string | undefined });
    const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { 'content-type': 'application/json' } });
    if (path === '/api/me') {
      return json({ username: 'aki', issuer: 'https://idp', subject: 'sub-aki', auth_mode: 'oidc', has_access: true, groups: [], roles: [], permissions });
    }
    if (path === '/api/settings') return json({ currency: 'EUR' });
    if (path === '/api/approvals') return json({ approvals, total: 2, limit: 50, offset: 0 });
    if (path.endsWith('/approve')) return json({ ...approvals[0], status: 'approved' });
    return json({ error: { code: 'not_found', message: 'not found' } }, 404);
  }));
  return calls;
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><ApprovalsPage /></QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('ApprovalsPage', () => {
  it('lets an approver approve requests of others but only withdraw their own', async () => {
    const calls = stubApi(['finance.read', 'finance.approve', 'finance.audit']);
    renderPage();
    expect(await screen.findByText('Payment for FIN-0001 on 2026-11-05')).toBeTruthy();
    expect(screen.getByText('Expense to Printer')).toBeTruthy();
    await waitFor(() => expect(screen.getAllByText('Approve')).toHaveLength(1));
    expect(screen.getByText('Withdraw')).toBeTruthy();
    expect(screen.getByText('Your request')).toBeTruthy();

    vi.spyOn(window, 'prompt').mockReturnValue('matches the bank statement');
    fireEvent.click(screen.getByText('Approve'));
    await waitFor(() => expect(calls.some((c) => c.url === '/api/approvals/a-theirs/approve' && c.method === 'POST')).toBe(true));
    const sent = calls.find((c) => c.url.endsWith('/approve'));
    expect(JSON.parse(sent!.body!)).toEqual({ note: 'matches the bank statement' });
  });

  it('shows no decision buttons to read-only users on requests of others', async () => {
    stubApi(['finance.read', 'finance.audit']);
    renderPage();
    expect(await screen.findByText('Payment for FIN-0001 on 2026-11-05')).toBeTruthy();
    await waitFor(() => expect(screen.queryByText('Approve')).toBeNull());
    expect(screen.queryByText('Reject')).toBeNull();
  });
});
