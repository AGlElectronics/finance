import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Pencil, Plus, Trash2 } from 'lucide-react';

import { ApiError } from '@/api/client';
import {
  EXPENSE_CATEGORIES,
  createExpense,
  deleteExpense,
  expenseCategoryLabel,
  listExpenses,
  updateExpense,
  type Expense,
  type ExpenseCategory,
  type ExpenseInput,
} from '@/api/expenses';
import { PAYMENT_METHODS, type PaymentMethod } from '@/api/invoices';
import { SearchInput } from '@/components/SearchInput';
import { centsToInput, formatCents, parseEuros } from '@/lib/money';
import { noAutofill } from '@/lib/noAutofill';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';

const today = () => new Date().toISOString().slice(0, 10);

type Form = {
  occurred_on: string;
  vendor: string;
  category: ExpenseCategory;
  amount: string; // euros as typed, e.g. "12,50"
  method: PaymentMethod;
  reference: string;
  description: string;
};

const emptyForm = (): Form => ({
  occurred_on: today(),
  vendor: '',
  category: 'other',
  amount: '',
  method: 'bank_transfer',
  reference: '',
  description: '',
});

function ExpenseForm({ initial, expenseId, onDone }: { initial: Form; expenseId?: string; onDone: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<Form>(initial);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const amountCents = parseEuros(form.amount);

  const save = useMutation({
    mutationFn: () => {
      const input: ExpenseInput = {
        occurred_on: form.occurred_on,
        vendor: form.vendor,
        category: form.category,
        amount_cents: amountCents ?? 0,
        method: form.method,
        reference: form.reference,
        description: form.description,
      };
      return expenseId ? updateExpense(expenseId, input) : createExpense(input);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['expenses'] });
      void qc.invalidateQueries({ queryKey: ['dashboard'] });
      void qc.invalidateQueries({ queryKey: ['ledger'] });
      onDone();
    },
  });
  const errs = save.error instanceof ApiError ? ((save.error.details as Record<string, string> | undefined) ?? {}) : {};

  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">{expenseId ? 'Edit expense' : 'New expense'}</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Vendor</span>
          <input {...noAutofill} required className={inputCls} value={form.vendor}
            onChange={(e) => set('vendor', e.target.value)} placeholder="e.g. Hall rental GmbH" />
          {errs.vendor && <span className="mt-1 block text-xs text-destructive">{errs.vendor}</span>}
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Amount (€)</span>
          <input {...noAutofill} required inputMode="decimal" className={inputCls} value={form.amount}
            onChange={(e) => set('amount', e.target.value)} placeholder="0,00" />
          {form.amount && amountCents === null && (
            <span className="mt-1 block text-xs text-destructive">Not a valid amount</span>
          )}
          {errs.amount_cents && <span className="mt-1 block text-xs text-destructive">{errs.amount_cents}</span>}
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Date</span>
          <input type="date" required className={inputCls} value={form.occurred_on}
            onChange={(e) => set('occurred_on', e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Category</span>
          <select className={inputCls} value={form.category}
            onChange={(e) => set('category', e.target.value as ExpenseCategory)}>
            {EXPENSE_CATEGORIES.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}
          </select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Paid via</span>
          <select className={inputCls} value={form.method}
            onChange={(e) => set('method', e.target.value as PaymentMethod)}>
            {PAYMENT_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Reference</span>
          <input {...noAutofill} className={inputCls} value={form.reference}
            onChange={(e) => set('reference', e.target.value)} placeholder="Bank statement reference" />
        </label>
        <label className="block text-sm sm:col-span-2 lg:col-span-3">
          <span className="mb-1 block font-medium">Description</span>
          <input {...noAutofill} className={inputCls} value={form.description}
            onChange={(e) => set('description', e.target.value)} />
        </label>
      </div>
      {save.error && Object.keys(errs).length === 0 && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !form.vendor.trim() || !amountCents || amountCents <= 0}
          className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : 'Save'}
        </button>
        <button type="button" onClick={onDone} className="rounded-pill border border-border px-4 py-2 text-sm">
          Cancel
        </button>
      </div>
    </form>
  );
}

export function ExpensesPage() {
  const qc = useQueryClient();
  const [category, setCategory] = useState<ExpenseCategory | ''>('');
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<Expense | 'new' | null>(null);

  const expenses = useQuery({
    queryKey: ['expenses', category],
    queryFn: () => listExpenses(category || undefined),
  });

  const remove = useMutation({
    mutationFn: (id: string) => deleteExpense(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['expenses'] });
      void qc.invalidateQueries({ queryKey: ['dashboard'] });
      void qc.invalidateQueries({ queryKey: ['ledger'] });
    },
  });

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const all = expenses.data ?? [];
    if (!q) return all;
    return all.filter((e) => `${e.vendor} ${e.description} ${e.reference}`.toLowerCase().includes(q));
  }, [expenses.data, search]);

  const totalCents = filtered.reduce((sum, e) => sum + e.amount_cents, 0);

  return (
    <div className="p-6 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Expenses</h1>
          <p className="text-sm text-muted-foreground">Money going out: rent, gear, travel, services.</p>
        </div>
        <button type="button" onClick={() => setEditing('new')}
          className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground">
          <Plus className="h-4 w-4" /> New expense
        </button>
      </div>

      {editing && (
        <ExpenseForm
          key={editing === 'new' ? 'new' : editing.id}
          expenseId={editing === 'new' ? undefined : editing.id}
          initial={editing === 'new' ? emptyForm() : {
            occurred_on: editing.occurred_on,
            vendor: editing.vendor,
            category: editing.category,
            amount: centsToInput(editing.amount_cents),
            method: editing.method,
            reference: editing.reference,
            description: editing.description,
          }}
          onDone={() => setEditing(null)}
        />
      )}

      <div className="flex flex-wrap items-center gap-3">
        <SearchInput value={search} onChange={setSearch} placeholder="Search expenses…" className="w-64" />
        <select className="rounded-md border border-input bg-background px-3 py-2 text-sm" value={category}
          onChange={(e) => setCategory(e.target.value as ExpenseCategory | '')}>
          <option value="">All categories</option>
          {EXPENSE_CATEGORIES.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}
        </select>
      </div>

      {expenses.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : expenses.error ? (
        <p className="text-destructive">Could not load expenses: {(expenses.error as Error).message}</p>
      ) : filtered.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">
          {search || category ? 'No expenses match your filters.' : 'No expenses yet — record the first one.'}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2.5 font-medium">Date</th>
                <th className="px-2 py-2.5 font-medium">Vendor</th>
                <th className="px-2 py-2.5 font-medium">Category</th>
                <th className="px-2 py-2.5 font-medium">Description</th>
                <th className="px-2 py-2.5 text-right font-medium">Amount</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {filtered.map((e) => (
                <tr key={e.id} className="border-b border-border last:border-b-0 hover:bg-muted/50">
                  <td className="px-4 py-2.5 font-mono text-xs">{e.occurred_on}</td>
                  <td className="px-2 py-2.5 font-medium">{e.vendor}</td>
                  <td className="px-2 py-2.5">
                    <span className="rounded-pill bg-muted px-2 py-0.5 text-xs">{expenseCategoryLabel(e.category)}</span>
                  </td>
                  <td className="px-2 py-2.5 text-muted-foreground">{e.description || e.reference || '—'}</td>
                  <td className="px-2 py-2.5 text-right font-medium tabular-nums text-destructive">
                    −{formatCents(e.amount_cents)}
                  </td>
                  <td className="px-4 py-2.5">
                    <div className="flex justify-end gap-1">
                      <button type="button" title="Edit" onClick={() => setEditing(e)}
                        className="rounded-pill p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button type="button" title="Delete"
                        onClick={() => {
                          if (confirm(`Delete this ${formatCents(e.amount_cents)} expense from ${e.vendor}?`)) remove.mutate(e.id);
                        }}
                        className="rounded-pill p-1.5 text-muted-foreground hover:bg-muted hover:text-destructive">
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr className="border-t border-border text-xs text-muted-foreground">
                <td className="px-4 py-2.5" colSpan={4}>
                  {filtered.length} expense{filtered.length === 1 ? '' : 's'}
                </td>
                <td className="px-2 py-2.5 text-right font-semibold tabular-nums text-foreground">
                  −{formatCents(totalCents)}
                </td>
                <td />
              </tr>
            </tfoot>
          </table>
        </div>
      )}
      {remove.error && <p className="text-sm text-destructive">{(remove.error as Error).message}</p>}
    </div>
  );
}
