import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Plus } from 'lucide-react';

import { getDashboard, type MonthFlow } from '@/api/dashboard';
import { InvoiceStatusBadge } from '@/components/InvoiceStatusBadge';
import { formatCents } from '@/lib/money';

function StatCard({ label, value, tone, hint }: { label: string; value: string; tone?: 'destructive' | 'success'; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className={`mt-1 text-2xl font-extrabold tracking-tight ${tone === 'destructive' ? 'text-destructive' : tone === 'success' ? 'text-success' : ''}`}>
        {value}
      </p>
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

function monthLabel(month: string): string {
  return new Date(`${month}-01T00:00:00`).toLocaleDateString(undefined, { month: 'short' });
}

function CashFlowChart({ months }: { months: MonthFlow[] }) {
  const max = Math.max(1, ...months.flatMap((m) => [m.in_cents, m.out_cents]));
  return (
    <section className="rounded-lg border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <h2 className="font-semibold">Cash flow, last 6 months</h2>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-sm bg-success" /> In</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-sm bg-destructive" /> Out</span>
        </div>
      </div>
      <div className="flex items-end justify-between gap-2 px-4 pb-3 pt-6">
        {months.map((m) => (
          <div key={m.month} className="flex flex-1 flex-col items-center gap-1.5">
            <div className="flex h-32 w-full max-w-20 items-end justify-center gap-1"
              title={`In ${formatCents(m.in_cents)} · Out ${formatCents(m.out_cents)}`}>
              <div className="w-1/2 rounded-t-sm bg-success"
                style={{ height: `${Math.max(m.in_cents > 0 ? 3 : 1, (m.in_cents / max) * 100)}%` }} />
              <div className="w-1/2 rounded-t-sm bg-destructive"
                style={{ height: `${Math.max(m.out_cents > 0 ? 3 : 1, (m.out_cents / max) * 100)}%` }} />
            </div>
            <span className="text-xs text-muted-foreground">{monthLabel(m.month)}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

export function DashboardPage() {
  const dash = useQuery({ queryKey: ['dashboard'], queryFn: getDashboard });

  if (dash.isPending) {
    return <div className="p-6 text-muted-foreground">Loading…</div>;
  }
  if (dash.error) {
    return <p className="p-6 text-destructive">Could not load the dashboard: {(dash.error as Error).message}</p>;
  }
  const d = dash.data;

  return (
    <div className="p-6 space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-sm text-muted-foreground">
            {d.open_count} open invoice{d.open_count === 1 ? '' : 's'} · {d.draft_count} draft{d.draft_count === 1 ? '' : 's'}
          </p>
        </div>
        <Link to="/invoices/new"
          className="inline-flex items-center gap-1.5 rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground">
          <Plus className="h-4 w-4" /> New invoice
        </Link>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <StatCard label="Still owed" value={formatCents(d.outstanding_cents)} hint="Issued invoices minus payments" />
        <StatCard label="Overdue" value={formatCents(d.overdue_cents)} tone={d.overdue_count > 0 ? 'destructive' : undefined}
          hint={`${d.overdue_count} invoice${d.overdue_count === 1 ? '' : 's'} past due`} />
        <StatCard label="Cash position" value={formatCents(d.net_total_cents)}
          tone={d.net_total_cents < 0 ? 'destructive' : undefined}
          hint={`In ${formatCents(d.received_total_cents)} · out ${formatCents(d.spent_total_cents)} all time`} />
        <StatCard label="Received this month" value={formatCents(d.received_month_cents)} tone="success" />
        <StatCard label="Spent this month" value={formatCents(d.spent_month_cents)}
          tone={d.spent_month_cents > 0 ? 'destructive' : undefined} />
        <StatCard label="Net this month" value={formatCents(d.received_month_cents - d.spent_month_cents)}
          tone={d.received_month_cents - d.spent_month_cents < 0 ? 'destructive' : 'success'} />
      </div>

      <CashFlowChart months={d.months} />

      <section className="rounded-lg border border-border bg-card">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="font-semibold">Recent invoices</h2>
          <Link to="/invoices" className="text-sm text-primary hover:underline">All invoices</Link>
        </div>
        {d.recent_invoices.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted-foreground">
            Nothing yet. Add a <Link to="/customers" className="text-primary hover:underline">customer</Link>, then write
            your first <Link to="/invoices/new" className="text-primary hover:underline">invoice</Link>.
          </p>
        ) : (
          <table className="w-full text-sm">
            <tbody>
              {d.recent_invoices.map((inv) => (
                <tr key={inv.id} className="border-b border-border last:border-b-0 hover:bg-muted/50">
                  <td className="px-4 py-2.5 font-mono text-xs">{inv.number || '—'}</td>
                  <td className="px-2 py-2.5">
                    <Link to="/invoices/$id" params={{ id: inv.id }} className="font-medium text-primary hover:underline">
                      {inv.customer_name}
                    </Link>
                  </td>
                  <td className="px-2 py-2.5"><InvoiceStatusBadge status={inv.status} overdue={inv.overdue} /></td>
                  <td className="px-4 py-2.5 text-right font-medium tabular-nums">{formatCents(inv.total_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
