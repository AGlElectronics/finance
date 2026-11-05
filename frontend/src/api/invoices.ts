import { apiRequest } from './client';

/** Derived by the API: draft | issued | partial | paid | void. */
export type InvoiceStatus = 'draft' | 'issued' | 'partial' | 'paid' | 'void';

export const INVOICE_STATUSES: { value: InvoiceStatus; label: string }[] = [
  { value: 'draft', label: 'Draft' },
  { value: 'issued', label: 'Issued' },
  { value: 'partial', label: 'Partially paid' },
  { value: 'paid', label: 'Paid' },
  { value: 'void', label: 'Void' },
];

export type PaymentMethod = 'bank_transfer' | 'cash' | 'card' | 'other';

export const PAYMENT_METHODS: { value: PaymentMethod; label: string }[] = [
  { value: 'bank_transfer', label: 'Bank transfer' },
  { value: 'cash', label: 'Cash' },
  { value: 'card', label: 'Card' },
  { value: 'other', label: 'Other' },
];

export type InvoiceLine = {
  id: string;
  description: string;
  quantity: number;
  unit_price_cents: number;
  tax_rate_bp: number;
  subtotal_cents: number;
  tax_cents: number;
  total_cents: number;
};

export type Payment = {
  id: string;
  paid_on: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
  note: string;
  created_at: string;
};

export type InvoiceSummary = {
  id: string;
  number?: string;
  customer_id: string;
  customer_name: string;
  status: InvoiceStatus;
  currency: string;
  issue_date?: string;
  due_date?: string;
  overdue: boolean;
  subtotal_cents: number;
  tax_cents: number;
  total_cents: number;
  paid_cents: number;
  created_at: string;
  updated_at: string;
};

export type Invoice = InvoiceSummary & {
  notes: string;
  lines: InvoiceLine[];
  payments: Payment[];
};

export type LineInput = {
  description: string;
  quantity: number;
  unit_price_cents: number;
  tax_rate_bp: number;
};

export type InvoiceInput = {
  customer_id: string;
  due_date?: string;
  notes: string;
  lines: LineInput[];
};

export type PaymentInput = {
  paid_on?: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
  note: string;
};

export const listInvoices = (opts: { status?: string; customerId?: string } = {}) =>
  apiRequest<{ invoices: InvoiceSummary[] }>('/api/invoices', {
    query: { status: opts.status || undefined, customer_id: opts.customerId || undefined },
  }).then((r) => r.invoices);

export const getInvoice = (id: string) => apiRequest<Invoice>(`/api/invoices/${id}`);

export const createInvoice = (input: InvoiceInput) =>
  apiRequest<Invoice>('/api/invoices', { method: 'POST', body: input });

export const updateInvoice = (id: string, input: InvoiceInput) =>
  apiRequest<Invoice>(`/api/invoices/${id}`, { method: 'PATCH', body: input });

export const deleteInvoice = (id: string) =>
  apiRequest<void>(`/api/invoices/${id}`, { method: 'DELETE' });

export const issueInvoice = (id: string, input: { issue_date?: string; due_date?: string } = {}) =>
  apiRequest<Invoice>(`/api/invoices/${id}/issue`, { method: 'POST', body: input });

export const voidInvoice = (id: string) =>
  apiRequest<Invoice>(`/api/invoices/${id}/void`, { method: 'POST', body: {} });

export const addPayment = (id: string, input: PaymentInput) =>
  apiRequest<Invoice>(`/api/invoices/${id}/payments`, { method: 'POST', body: input });
