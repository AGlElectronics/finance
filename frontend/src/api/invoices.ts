import type { Approval, Pending } from './approvals';
import { apiRequest, downloadFile, newIdempotencyKey } from './client';
import type { Seller } from './settings';

/** Derived by the API: draft | issued | partial | paid | credited | void. */
export type InvoiceStatus = 'draft' | 'issued' | 'partial' | 'paid' | 'credited' | 'void';

export const INVOICE_STATUSES: { value: InvoiceStatus; label: string }[] = [
  { value: 'draft', label: 'Draft' },
  { value: 'issued', label: 'Issued' },
  { value: 'partial', label: 'Partially paid' },
  { value: 'paid', label: 'Paid' },
  { value: 'credited', label: 'Credited' },
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
  credited_cents: number;
  refunded_cents: number;
  due_cents: number;
  refundable_cents: number;
  created_at: string;
  updated_at: string;
};

export type Party = { name: string; email: string; address: string; tax_id: string };

export type CreditNoteSummary = {
  id: string;
  number: string;
  invoice_id: string;
  issue_date: string;
  reason: string;
  currency: string;
  subtotal_cents: number;
  tax_cents: number;
  total_cents: number;
  refunded_cents: number;
  created_at: string;
};

export type Refund = {
  id: string;
  credit_note_id: string;
  invoice_id: string;
  refunded_on: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
  note: string;
  created_at: string;
};

export type Invoice = InvoiceSummary & {
  notes: string;
  customer: Party;
  seller: Seller | null;
  has_pdf: boolean;
  lines: InvoiceLine[];
  payments: Payment[];
  credit_notes: CreditNoteSummary[];
  refunds: Refund[];
  pending_approvals: Approval[];
};

export type CreditNote = CreditNoteSummary & {
  invoice_number: string;
  customer: Party;
  seller: Seller;
  lines: InvoiceLine[];
  refunds: Refund[];
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

export type CreditNoteInput = { reason: string; issue_date?: string; full?: boolean; lines?: LineInput[] };

export type RefundInput = {
  refunded_on?: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
  note: string;
};

export type InvoicePage = { invoices: InvoiceSummary[]; total: number; limit: number; offset: number };

export const listInvoices = (opts: { status?: string; customerId?: string; limit?: number; offset?: number } = {}) =>
  apiRequest<InvoicePage>('/api/invoices', {
    query: { status: opts.status || undefined, customer_id: opts.customerId || undefined, limit: opts.limit, offset: opts.offset },
  });

export const getInvoice = (id: string) => apiRequest<Invoice>(`/api/invoices/${id}`);

const idem = (key: string) => ({ 'Idempotency-Key': key });

export const createInvoice = (input: InvoiceInput, key = newIdempotencyKey()) =>
  apiRequest<Invoice>('/api/invoices', { method: 'POST', body: input, headers: idem(key) });

export const updateInvoice = (id: string, input: InvoiceInput) =>
  apiRequest<Invoice>(`/api/invoices/${id}`, { method: 'PATCH', body: input });

export const deleteInvoice = (id: string) =>
  apiRequest<void>(`/api/invoices/${id}`, { method: 'DELETE' });

export const issueInvoice = (id: string, input: { issue_date?: string; due_date?: string } = {}) =>
  apiRequest<Invoice>(`/api/invoices/${id}/issue`, { method: 'POST', body: input });

export const voidInvoice = (id: string, reason: string) =>
  apiRequest<Invoice | Pending>(`/api/invoices/${id}/void`, { method: 'POST', body: { reason } });

export const addPayment = (id: string, input: PaymentInput, key = newIdempotencyKey()) =>
  apiRequest<Invoice | Pending>(`/api/invoices/${id}/payments`, { method: 'POST', body: input, headers: idem(key) });

export const createCreditNote = (invoiceId: string, input: CreditNoteInput, key = newIdempotencyKey()) =>
  apiRequest<CreditNote | Pending>(`/api/invoices/${invoiceId}/credit-notes`, { method: 'POST', body: input, headers: idem(key) });

export const getCreditNote = (id: string) => apiRequest<CreditNote>(`/api/credit-notes/${id}`);

export const createRefund = (creditNoteId: string, input: RefundInput, key = newIdempotencyKey()) =>
  apiRequest<CreditNote | Pending>(`/api/credit-notes/${creditNoteId}/refunds`, { method: 'POST', body: input, headers: idem(key) });

export const downloadInvoicePdf = (inv: { id: string; number?: string }) =>
  downloadFile(`/api/invoices/${inv.id}/pdf`, `${inv.number ?? 'invoice'}.pdf`);

export const downloadCreditNotePdf = (cn: { id: string; number: string }) =>
  downloadFile(`/api/credit-notes/${cn.id}/pdf`, `${cn.number}.pdf`);
