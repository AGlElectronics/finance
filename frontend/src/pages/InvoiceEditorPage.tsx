import { useEffect, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { Plus, Trash2 } from 'lucide-react';

import { ApiError, newIdempotencyKey } from '@/api/client';
import { listCustomers } from '@/api/customers';
import { createInvoice, getInvoice, updateInvoice, type InvoiceInput } from '@/api/invoices';
import { useSettings } from '@/api/settings';
import { centsToInput, formatCents, parseEuros } from '@/lib/money';
import { queryClient } from '@/lib/queryClient';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';

/** A line while editing: money and tax stay as text so typing is not fought. */
type LineDraft = { description: string; quantity: string; unitPrice: string; taxPercent: string };

const emptyLine = (vatBp: number): LineDraft => ({ description: '', quantity: '1', unitPrice: '', taxPercent: String(vatBp / 100) });

function lineCents(l: LineDraft): { subtotal: number; tax: number } | null {
  const qty = Number(l.quantity.replace(',', '.'));
  const unit = parseEuros(l.unitPrice);
  const taxBp = parseEuros(l.taxPercent); // percent * 100 = basis points
  if (!Number.isFinite(qty) || qty <= 0 || unit === null || taxBp === null || taxBp < 0 || taxBp > 10000) return null;
  // Mirror the backend: quantity in hundredths, round half away from zero.
  const qtyH = Math.round(qty * 100);
  const subtotal = Math.round((qtyH * unit) / 100);
  const tax = Math.round((subtotal * taxBp) / 10000);
  return { subtotal, tax };
}

function toInput(customerId: string, dueDate: string, notes: string, lines: LineDraft[]): InvoiceInput | null {
  if (!customerId) return null;
  const out: InvoiceInput = { customer_id: customerId, due_date: dueDate || undefined, notes, lines: [] };
  for (const l of lines) {
    const qty = Number(l.quantity.replace(',', '.'));
    const unit = parseEuros(l.unitPrice);
    const taxBp = parseEuros(l.taxPercent);
    if (!l.description.trim() || !Number.isFinite(qty) || qty <= 0 || unit === null || taxBp === null) return null;
    out.lines.push({
      description: l.description.trim(),
      quantity: qty,
      unit_price_cents: unit,
      tax_rate_bp: taxBp,
    });
  }
  return out.lines.length > 0 ? out : null;
}

export function InvoiceEditorPage() {
  // '/invoices/new' has no id; '/invoices/$id/edit' does.
  const { id } = useParams({ strict: false }) as { id?: string };
  const navigate = useNavigate();

  const customers = useQuery({ queryKey: ['customers', false], queryFn: () => listCustomers(false) });
  const existing = useQuery({
    queryKey: ['invoice', id],
    queryFn: () => getInvoice(id!),
    enabled: Boolean(id),
  });

  const settings = useSettings();
  const vatBp = settings.data?.default_vat_rate_bp ?? 2000;
  const currency = settings.data?.currency ?? 'EUR';
  const [customerId, setCustomerId] = useState('');
  const [dueDate, setDueDate] = useState('');
  const [notes, setNotes] = useState('');
  const [lines, setLines] = useState<LineDraft[]>([emptyLine(vatBp)]);
  const [loaded, setLoaded] = useState(false);
  // One key per new draft: a retry after a network error does not create it twice.
  const [key] = useState(newIdempotencyKey);

  useEffect(() => {
    if (id || !settings.data) return;
    setLines((ls) => (ls.length === 1 && !ls[0].description && !ls[0].unitPrice ? [emptyLine(settings.data.default_vat_rate_bp)] : ls));
  }, [id, settings.data]);

  useEffect(() => {
    if (!id || !existing.data || loaded) return;
    const inv = existing.data;
    setCustomerId(inv.customer_id);
    setDueDate(inv.due_date ?? '');
    setNotes(inv.notes);
    setLines(
      inv.lines.map((l) => ({
        description: l.description,
        quantity: String(l.quantity),
        unitPrice: centsToInput(l.unit_price_cents),
        taxPercent: (l.tax_rate_bp / 100).toString(),
      })),
    );
    setLoaded(true);
  }, [id, existing.data, loaded]);

  const setLine = (i: number, patch: Partial<LineDraft>) =>
    setLines((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  const save = useMutation({
    mutationFn: (input: InvoiceInput) => (id ? updateInvoice(id, input) : createInvoice(input, key)),
    onSuccess: (inv) => {
      void queryClient.invalidateQueries({ queryKey: ['invoices'] });
      void queryClient.invalidateQueries({ queryKey: ['invoice', inv.id] });
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
      void navigate({ to: '/invoices/$id', params: { id: inv.id } });
    },
  });
  const errs = save.error instanceof ApiError ? ((save.error.details as Record<string, string> | undefined) ?? {}) : {};

  if (id && existing.isPending) {
    return <div className="p-6 text-muted-foreground">Loading…</div>;
  }
  if (id && existing.error) {
    return <p className="p-6 text-destructive">Could not load the invoice: {(existing.error as Error).message}</p>;
  }
  if (id && existing.data && existing.data.status !== 'draft') {
    return (
      <div className="p-6 space-y-3">
        <h1 className="text-xl font-bold">This invoice is {existing.data.status}</h1>
        <p className="text-sm text-muted-foreground">Only drafts can be edited.</p>
        <Link to="/invoices/$id" params={{ id }} className="text-sm text-primary hover:underline">Back to the invoice</Link>
      </div>
    );
  }

  const totals = lines.reduce(
    (acc, l) => {
      const c = lineCents(l);
      if (!c) return acc;
      return { subtotal: acc.subtotal + c.subtotal, tax: acc.tax + c.tax };
    },
    { subtotal: 0, tax: 0 },
  );
  const input = toInput(customerId, dueDate, notes, lines);

  return (
    <div className="p-6 max-w-3xl space-y-5">
      <div>
        <h1 className="text-2xl font-bold">{id ? 'Edit draft' : 'New invoice'}</h1>
        <p className="text-sm text-muted-foreground">Saved as a draft — nothing is numbered until you issue it.</p>
      </div>

      <form className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault();
          if (input) save.mutate(input);
        }}>
        <section className="grid gap-3 rounded-lg border border-border bg-card p-4 sm:grid-cols-2">
          <label className="block text-sm">
            <span className="mb-1 block font-medium">Customer</span>
            <select required className={inputCls} value={customerId} onChange={(e) => setCustomerId(e.target.value)}>
              <option value="" disabled>Pick a customer…</option>
              {(customers.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
            {errs.customer_id && <span className="mt-1 block text-xs text-destructive">{errs.customer_id}</span>}
            {customers.data?.length === 0 && (
              <span className="mt-1 block text-xs text-muted-foreground">
                No customers yet — <Link to="/customers" className="text-primary hover:underline">create one first</Link>.
              </span>
            )}
          </label>
          <label className="block text-sm">
            <span className="mb-1 block font-medium">Due date</span>
            <input type="date" className={inputCls} value={dueDate} onChange={(e) => setDueDate(e.target.value)} />
            <span className="mt-1 block text-xs text-muted-foreground">Empty = 30 days after issuing.</span>
          </label>
          <label className="block text-sm sm:col-span-2">
            <span className="mb-1 block font-medium">Notes</span>
            <textarea className={`${inputCls} min-h-16`} value={notes} onChange={(e) => setNotes(e.target.value)}
              placeholder="Shown on the invoice, e.g. payment terms." />
          </label>
        </section>

        <section className="space-y-3 rounded-lg border border-border bg-card p-4">
          <div className="flex items-center justify-between">
            <h2 className="font-semibold">Lines</h2>
            <button type="button" onClick={() => setLines((ls) => [...ls, emptyLine(vatBp)])}
              className="inline-flex items-center gap-1 rounded-pill border border-border px-3 py-1.5 text-sm">
              <Plus className="h-4 w-4" /> Add line
            </button>
          </div>

          <div className="space-y-2">
            <div className="hidden gap-2 text-xs font-medium text-muted-foreground sm:grid sm:grid-cols-[1fr_5rem_7rem_5rem_6rem_2rem]">
              <span>Description</span><span>Qty</span><span>Unit price ({currency})</span><span>Tax %</span>
              <span className="text-right">Total</span><span />
            </div>
            {lines.map((l, i) => {
              const c = lineCents(l);
              return (
                <div key={i} className="grid gap-2 sm:grid-cols-[1fr_5rem_7rem_5rem_6rem_2rem] sm:items-center">
                  <input className={inputCls} placeholder="What was delivered"
                    value={l.description} onChange={(e) => setLine(i, { description: e.target.value })} />
                  <input className={inputCls} inputMode="decimal" placeholder="1"
                    value={l.quantity} onChange={(e) => setLine(i, { quantity: e.target.value })} />
                  <input className={inputCls} inputMode="decimal" placeholder="0,00"
                    value={l.unitPrice} onChange={(e) => setLine(i, { unitPrice: e.target.value })} />
                  <input className={inputCls} inputMode="decimal" placeholder={String(vatBp / 100)}
                    value={l.taxPercent} onChange={(e) => setLine(i, { taxPercent: e.target.value })} />
                  <span className="text-right text-sm tabular-nums text-muted-foreground">
                    {c ? formatCents(c.subtotal + c.tax, currency) : '—'}
                  </span>
                  <button type="button" title="Remove line" disabled={lines.length === 1}
                    onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))}
                    className="justify-self-end rounded-pill p-1.5 text-muted-foreground hover:text-destructive disabled:opacity-40">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              );
            })}
          </div>

          <div className="ml-auto w-56 space-y-1 border-t border-border pt-3 text-sm">
            <div className="flex justify-between"><span className="text-muted-foreground">Subtotal</span><span className="tabular-nums">{formatCents(totals.subtotal, currency)}</span></div>
            <div className="flex justify-between"><span className="text-muted-foreground">Tax</span><span className="tabular-nums">{formatCents(totals.tax, currency)}</span></div>
            <div className="flex justify-between font-semibold"><span>Total</span><span className="tabular-nums">{formatCents(totals.subtotal + totals.tax, currency)}</span></div>
          </div>
        </section>

        {save.error && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
        <div className="flex gap-2">
          <button type="submit" disabled={!input || save.isPending}
            className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
            {save.isPending ? 'Saving…' : 'Save draft'}
          </button>
          <Link to={id ? '/invoices/$id' : '/invoices'} params={id ? { id } : undefined}
            className="rounded-pill border border-border px-4 py-2 text-sm">
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}
