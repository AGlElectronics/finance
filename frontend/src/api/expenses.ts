import { apiRequest } from './client';
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

export const listExpenses = (category?: ExpenseCategory) =>
  apiRequest<{ expenses: Expense[] }>('/api/expenses', {
    query: category ? { category } : undefined,
  }).then((r) => r.expenses);

export const createExpense = (input: ExpenseInput) =>
  apiRequest<Expense>('/api/expenses', { method: 'POST', body: input });

export const updateExpense = (id: string, input: ExpenseInput) =>
  apiRequest<Expense>(`/api/expenses/${id}`, { method: 'PATCH', body: input });

export const deleteExpense = (id: string) =>
  apiRequest<void>(`/api/expenses/${id}`, { method: 'DELETE' });
