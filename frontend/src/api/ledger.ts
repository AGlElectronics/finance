import { apiRequest } from './client';

export type LedgerEntry = {
  id: string;
  day: string; // YYYY-MM-DD
  direction: 'in' | 'out';
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
};

export const getLedger = () => apiRequest<Ledger>('/api/ledger');
