import type { Pending } from './approvals';
import { apiRequest, newIdempotencyKey } from './client';
import type { PaymentMethod } from './invoices';

export type ExpenseCategory =
  | 'venue'
  | 'equipment'
  | 'travel'
  | 'supplies'
  | 'services'
  | 'fees'
  | 'other';

export const EXPENSE_CATEGORIES: { value: ExpenseCategory; label: string }[] = [
  { value: 'venue', label: 'Venue & rent' },
  { value: 'equipment', label: 'Equipment' },
  { value: 'travel', label: 'Travel' },
  { value: 'supplies', label: 'Supplies' },
  { value: 'services', label: 'Services' },
  { value: 'fees', label: 'Fees & charges' },
  { value: 'other', label: 'Other' },
];

export const expenseCategoryLabel = (value: string) =>
  EXPENSE_CATEGORIES.find((c) => c.value === value)?.label ?? value;

export type Expense = {
  id: string;
  occurred_on: string; // YYYY-MM-DD
  vendor: string;
  category: ExpenseCategory;
  description: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
  created_at: string;
  updated_at: string;
  deleted: boolean;
  deleted_at?: string;
  deleted_reason?: string;
};

export type ExpenseInput = Partial<{
  occurred_on: string;
  vendor: string;
  category: ExpenseCategory;
  description: string;
  amount_cents: number;
  method: PaymentMethod;
  reference: string;
}>;

export type ExpensePage = { expenses: Expense[]; total: number; limit: number; offset: number };

export const listExpenses = (opts: { category?: ExpenseCategory; includeDeleted?: boolean; limit?: number; offset?: number } = {}) =>
  apiRequest<ExpensePage>('/api/expenses', {
    query: {
      category: opts.category || undefined,
      include_deleted: opts.includeDeleted || undefined,
      limit: opts.limit,
      offset: opts.offset,
    },
  });

/** Records an expense; above the approval threshold it waits for a second person. */
export const createExpense = (input: ExpenseInput, key = newIdempotencyKey()) =>
  apiRequest<Expense | Pending>('/api/expenses', { method: 'POST', body: input, headers: { 'Idempotency-Key': key } });

/** The amount and date are fixed once recorded; send only the other fields. */
export const updateExpense = (id: string, input: ExpenseInput) =>
  apiRequest<Expense>(`/api/expenses/${id}`, { method: 'PATCH', body: input });

export const deleteExpense = (id: string, reason: string) =>
  apiRequest<void>(`/api/expenses/${id}`, { method: 'DELETE', body: { reason } });
