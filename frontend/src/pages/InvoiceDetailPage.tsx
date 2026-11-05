import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { Ban, Pencil, Printer, Send, Trash2 } from 'lucide-react';

import { ApiError } from '@/api/client';
import {
  addPayment,
  deleteInvoice,
  getInvoice,
  issueInvoice,
  voidInvoice,
  PAYMENT_METHODS,
  type Invoice,
  type PaymentMethod,
} from '@/api/invoices';
import { InvoiceStatusBadge } from '@/components/InvoiceStatusBadge';
import { centsToInput, formatCents, formatTaxBp, parseEuros } from '@/lib/money';
import { queryClient } from '@/lib/queryClient';

const inputCls = 'w-full rounded-md border border-input bg-background px-3 py-2 text-sm';

function invalidate(id: string) {
  void queryClient.invalidateQueries({ queryKey: ['invoice', id] });
  void queryClient.invalidateQueries({ queryKey: ['invoices'] });
  void queryClient.invalidateQueries({ queryKey: ['dashboard'] });
  void queryClient.invalidateQueries({ queryKey: ['customers'] });
}

function PaymentForm({ invoice, onDone }: { invoice: Invoice; onDone: () => void }) {
  const remaining = invoice.total_cents - invoice.paid_cents;
  const [amount, setAmount] = useState(centsToInput(remaining));
  const [paidOn, setPaidOn] = useState(new Date().toISOString().slice(0, 10));
  const [method, setMethod] = useState<PaymentMethod>('bank_transfer');
  const [reference, setReference] = useState('');
  const [note, setNote] = useState('');

  const save = useMutation({
    mutationFn: () =>
      addPayment(invoice.id, {
        paid_on: paidOn,
        amount_cents: parseEuros(amount) ?? 0,
        method,
        reference,
        note,
      }),
    onSuccess: () => {
      invalidate(invoice.id);
      onDone();
    },
  });
  const errs = save.error instanceof ApiError ? ((save.error.details as Record<string, string> | undefined) ?? {}) : {};
  const cents = parseEuros(amount);

  return (
    <form className="space-y-3 rounded-lg border border-border bg-card p-4 print:hidden"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}>
      <h2 className="font-semibold">Record a payment</h2>
      <p className="text-xs text-muted-foreground">Still owed: {formatCents(remaining)}. A later bank feed would match on method + reference.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Amount €</span>
          <input className={inputCls} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
          {errs.amount_cents && <span className="mt-1 block text-xs text-destructive">{errs.amount_cents}</span>}
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Received on</span>
          <input type="date" className={inputCls} value={paidOn} onChange={(e) => setPaidOn(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Method</span>
          <select className={inputCls} value={method} onChange={(e) => setMethod(e.target.value as PaymentMethod)}>
            {PAYMENT_METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Reference</span>
          <input className={inputCls} value={reference} onChange={(e) => setReference(e.target.value)}
            placeholder="e.g. bank statement reference" />
        </label>
        <label className="block text-sm sm:col-span-2">
          <span className="mb-1 block font-medium">Note</span>
          <input className={inputCls} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
      </div>
      {save.error && Object.keys(errs).length === 0 && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
      <div className="flex gap-2">
        <button type="submit" disabled={save.isPending || !cents || cents <= 0}
          className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground disabled:opacity-60">
          {save.isPending ? 'Saving…' : 'Record payment'}
        </button>
        <button type="button" onClick={onDone} className="rounded-pill border border-border px-4 py-2 text-sm">Cancel</button>
      </div>
    </form>
  );
}

export function InvoiceDetailPage() {
  const { id } = useParams({ from: '/app/invoices/$id' });
  const navigate = useNavigate();
  const [paying, setPaying] = useState(false);

  const invoice = useQuery({ queryKey: ['invoice', id], queryFn: () => getInvoice(id) });

  const issue = useMutation({
    mutationFn: () => issueInvoice(id),
    onSuccess: () => invalidate(id),
  });
  const voidIt = useMutation({
    mutationFn: () => voidInvoice(id),
    onSuccess: () => invalidate(id),
  });
  const remove = useMutation({
    mutationFn: () => deleteInvoice(id),
    onSuccess: () => {
      invalidate(id);
      void navigate({ to: '/invoices', search: { status: undefined, customer: undefined } });
    },
  });

  if (invoice.isPending) {
    return <div className="p-6 text-muted-foreground">Loading…</div>;
  }
  if (invoice.error) {
    return <p className="p-6 text-destructive">Could not load the invoice: {(invoice.error as Error).message}</p>;
  }
  const inv = invoice.data;
  const remaining = inv.total_cents - inv.paid_cents;
  const actionError = (issue.error ?? voidIt.error ?? remove.error) as Error | null;

  return (
    <div className="p-6 max-w-3xl space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold">{inv.number || 'Draft invoice'}</h1>
            <InvoiceStatusBadge status={inv.status} overdue={inv.overdue} />
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            {inv.customer_name}
            {inv.issue_date && <> · issued {inv.issue_date}</>}
            {inv.due_date && <> · due {inv.due_date}</>}
          </p>
        </div>

        <div className="flex flex-wrap gap-2 print:hidden">
          {inv.status === 'draft' && (
            <>
              <Link to="/invoices/$id/edit" params={{ id }}
                className="inline-flex items-center gap-1.5 rounded-pill border border-border px-3 py-1.5 text-sm">
                <Pencil className="h-4 w-4" /> Edit
              </Link>
              <button type="button" disabled={issue.isPending}
                onClick={() => {
                  if (confirm('Issue this invoice? It gets the next number and the lines are locked.')) issue.mutate();
                }}
                className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground disabled:opacity-60">
                <Send className="h-4 w-4" /> {issue.isPending ? 'Issuing…' : 'Issue'}
              </button>
              <button type="button" disabled={remove.isPending}
                onClick={() => {
                  if (confirm('Delete this draft? This cannot be undone.')) remove.mutate();
                }}
                className="inline-flex items-center gap-1.5 rounded-pill border border-destructive/40 px-3 py-1.5 text-sm text-destructive disabled:opacity-60">
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            </>
          )}
          {(inv.status === 'issued' || inv.status === 'partial') && (
            <>
              <button type="button" onClick={() => setPaying(true)}
                className="rounded-pill bg-primary px-3 py-1.5 text-sm font-semibold text-primary-foreground">
                Record payment
              </button>
              <button type="button" disabled={voidIt.isPending}
                onClick={() => {
                  if (confirm('Void this invoice? Its number stays taken and it stops counting as owed.')) voidIt.mutate();
                }}
                className="inline-flex items-center gap-1.5 rounded-pill border border-destructive/40 px-3 py-1.5 text-sm text-destructive disabled:opacity-60">
                <Ban className="h-4 w-4" /> Void
              </button>
            </>
          )}
          {inv.status !== 'draft' && (
            <button type="button" onClick={() => window.print()}
              className="inline-flex items-center gap-1.5 rounded-pill border border-border px-3 py-1.5 text-sm">
              <Printer className="h-4 w-4" /> Print
            </button>
          )}
        </div>
      </div>

      {actionError && <p className="text-sm text-destructive print:hidden">{actionError.message}</p>}
      {paying && (inv.status === 'issued' || inv.status === 'partial') && (
        <PaymentForm invoice={inv} onDone={() => setPaying(false)} />
      )}

      <section className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="px-4 py-2.5 font-medium">Description</th>
              <th className="px-2 py-2.5 text-right font-medium">Qty</th>
              <th className="px-2 py-2.5 text-right font-medium">Unit price</th>
              <th className="px-2 py-2.5 text-right font-medium">Tax</th>
              <th className="px-4 py-2.5 text-right font-medium">Total</th>
            </tr>
          </thead>
          <tbody>
            {inv.lines.map((l) => (
              <tr key={l.id} className="border-b border-border last:border-b-0">
                <td className="px-4 py-2.5">{l.description}</td>
                <td className="px-2 py-2.5 text-right tabular-nums">{l.quantity}</td>
                <td className="px-2 py-2.5 text-right tabular-nums">{formatCents(l.unit_price_cents)}</td>
                <td className="px-2 py-2.5 text-right tabular-nums text-muted-foreground">{formatTaxBp(l.tax_rate_bp)}</td>
                <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(l.total_cents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="ml-auto w-64 space-y-1 px-4 py-3 text-sm">
          <div className="flex justify-between"><span className="text-muted-foreground">Subtotal</span><span className="tabular-nums">{formatCents(inv.subtotal_cents)}</span></div>
          <div className="flex justify-between"><span className="text-muted-foreground">Tax</span><span className="tabular-nums">{formatCents(inv.tax_cents)}</span></div>
          <div className="flex justify-between font-semibold"><span>Total</span><span className="tabular-nums">{formatCents(inv.total_cents)}</span></div>
          {inv.paid_cents > 0 && (
            <>
              <div className="flex justify-between text-success"><span>Paid</span><span className="tabular-nums">−{formatCents(inv.paid_cents)}</span></div>
              <div className="flex justify-between font-semibold"><span>Still owed</span><span className="tabular-nums">{formatCents(remaining)}</span></div>
            </>
          )}
        </div>
      </section>

      {inv.notes && (
        <section className="rounded-lg border border-border bg-card p-4 text-sm">
          <h2 className="mb-1 font-semibold">Notes</h2>
          <p className="whitespace-pre-wrap text-muted-foreground">{inv.notes}</p>
        </section>
      )}

      {inv.payments.length > 0 && (
        <section className="rounded-lg border border-border bg-card">
          <h2 className="border-b border-border px-4 py-3 font-semibold">Payments</h2>
          <table className="w-full text-sm">
            <tbody>
              {inv.payments.map((p) => (
                <tr key={p.id} className="border-b border-border last:border-b-0">
                  <td className="px-4 py-2.5 text-muted-foreground">{p.paid_on}</td>
                  <td className="px-2 py-2.5">{PAYMENT_METHODS.find((m) => m.value === p.method)?.label ?? p.method}</td>
                  <td className="px-2 py-2.5 font-mono text-xs text-muted-foreground">{p.reference || '—'}</td>
                  <td className="px-2 py-2.5 text-muted-foreground">{p.note}</td>
                  <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(p.amount_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      <Link to="/invoices" search={{ status: undefined, customer: undefined }}
        className="inline-block text-sm text-primary hover:underline print:hidden">
        ← All invoices
      </Link>
    </div>
  );
}
