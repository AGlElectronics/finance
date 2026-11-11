import { apiRequest } from './client';

export type ApprovalAction = 'invoice.void' | 'payment.record' | 'expense.create' | 'credit_note.issue' | 'refund.record';

export const APPROVAL_ACTION_LABELS: Record<ApprovalAction, string> = {
  'invoice.void': 'Void invoice',
  'payment.record': 'Record payment',
  'expense.create': 'Record expense',
  'credit_note.issue': 'Issue credit note',
  'refund.record': 'Pay refund',
};

export type Person = { name: string; issuer: string; subject: string };

export type Approval = {
  id: string;
  action: ApprovalAction;
  target_id: string | null;
  target_number?: string;
  amount_cents: number;
  payload: Record<string, unknown>;
  status: 'pending' | 'approved' | 'rejected';
  requested_by: Person;
  requested_at: string;
  decided_by: Person | null;
  decided_at: string | null;
  decision_note: string;
  result_id: string | null;
};

/** A create or money request either happened (the resource) or waits for a second person. */
export type Pending = { status: 'pending_approval'; approval: Approval };

export const isPending = (v: unknown): v is Pending =>
  typeof v === 'object' && v !== null && (v as { status?: unknown }).status === 'pending_approval';

export type Paged = { total: number; limit: number; offset: number };

export const listApprovals = (opts: { status?: string; limit?: number; offset?: number } = {}) =>
  apiRequest<Paged & { approvals: Approval[] }>('/api/approvals', {
    query: { status: opts.status || undefined, limit: opts.limit, offset: opts.offset },
  });

export const approve = (id: string, note = '') =>
  apiRequest<Approval>(`/api/approvals/${id}/approve`, { method: 'POST', body: { note } });

export const reject = (id: string, note: string) =>
  apiRequest<Approval>(`/api/approvals/${id}/reject`, { method: 'POST', body: { note } });

/** One line saying what would happen, from the stored request. */
export function describeApproval(a: Approval): string {
  const p = a.payload as Record<string, string | number | boolean | undefined>;
  const on = (k: string) => (p[k] ? ` on ${p[k]}` : '');
  switch (a.action) {
    case 'invoice.void':
      return `Void ${a.target_number || 'invoice'}: ${p.reason ?? ''}`;
    case 'payment.record':
      return `Payment for ${a.target_number || 'invoice'}${on('paid_on')}${p.reference ? ` (ref ${p.reference})` : ''}`;
    case 'credit_note.issue':
      return `Credit note for ${a.target_number || 'invoice'}${p.full ? ' (whole invoice)' : ''}: ${p.reason ?? ''}`;
    case 'refund.record':
      return `Refund against ${a.target_number || 'credit note'}${on('refunded_on')}`;
    case 'expense.create':
      return `Expense to ${p.vendor ?? '?'}${on('occurred_on')}${p.description ? `: ${p.description}` : ''}`;
  }
}

/** Same person check as the backend: issuer and subject, else the name. */
export const samePerson = (a: Person, b: { username: string; issuer?: string; subject?: string }) =>
  a.subject && b.subject ? a.issuer === b.issuer && a.subject === b.subject : a.name === b.username;
