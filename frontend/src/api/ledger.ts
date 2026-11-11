import { apiRequest } from './client';

export type LedgerEntry = {
  id: string;
  day: string; // YYYY-MM-DD
  direction: 'in' | 'out';
  kind: 'payment' | 'expense' | 'refund';
  amount_cents: number;
  counterparty: string;
  detail: string;
  method: string;
  invoice_id?: string;
  balance_cents: number;
};

export type Ledger = {
  in_cents: number;
  out_cents: number;
  net_cents: number;
  entries: LedgerEntry[];
  total: number;
  limit: number;
  offset: number;
};

export const getLedger = (page: { limit?: number; offset?: number } = {}) =>
  apiRequest<Ledger>('/api/ledger', { query: page });
