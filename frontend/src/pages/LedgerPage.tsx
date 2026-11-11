import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowDownLeft, ArrowUpRight } from 'lucide-react';

import { expenseCategoryLabel } from '@/api/expenses';
import { getLedger, type LedgerEntry } from '@/api/ledger';
import { PAGE_SIZE, Pagination } from '@/components/Pagination';
import { SearchInput } from '@/components/SearchInput';
import { formatCents } from '@/lib/money';

function TotalCard({ label, cents, tone }: { label: string; cents: number; tone?: 'success' | 'destructive' }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className={`mt-1 text-2xl font-extrabold tracking-tight tabular-nums ${
        tone === 'success' ? 'text-success' : tone === 'destructive' ? 'text-destructive' : ''}`}>
        {formatCents(cents)}
      </p>
    </div>
  );
}

function DirectionBadge({ direction }: { direction: LedgerEntry['direction'] }) {
  return direction === 'in' ? (
    <span className="inline-flex items-center gap-1 rounded-pill bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
      <ArrowDownLeft className="h-3 w-3" /> In
    </span>
  ) : (
    <span className="inline-flex items-center gap-1 rounded-pill bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
      <ArrowUpRight className="h-3 w-3" /> Out
    </span>
  );
}

export function LedgerPage() {
  const [offset, setOffset] = useState(0);
  const ledger = useQuery({ queryKey: ['ledger', offset], queryFn: () => getLedger({ limit: PAGE_SIZE, offset }) });
  const [search, setSearch] = useState('');
  const [direction, setDirection] = useState<'' | 'in' | 'out'>('');

  const filtered = useMemo(() => {
    const all = ledger.data?.entries ?? [];
    const q = search.trim().toLowerCase();
    return all.filter((e) =>
      (!direction || e.direction === direction) &&
      (!q || `${e.counterparty} ${e.detail} ${e.method}`.toLowerCase().includes(q)));
  }, [ledger.data, search, direction]);

  if (ledger.isPending) {
    return <div className="p-6 text-muted-foreground">Loading…</div>;
  }
  if (ledger.error) {
    return <p className="p-6 text-destructive">Could not load the ledger: {(ledger.error as Error).message}</p>;
  }
  const l = ledger.data;

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Ledger</h1>
        <p className="text-sm text-muted-foreground">Every payment in and every expense and refund out, with the running cash position.</p>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <TotalCard label="Money in" cents={l.in_cents} tone="success" />
        <TotalCard label="Money out" cents={l.out_cents} tone="destructive" />
        <TotalCard label="Cash position" cents={l.net_cents} tone={l.net_cents < 0 ? 'destructive' : undefined} />
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <SearchInput value={search} onChange={setSearch} placeholder="Search the ledger…" className="w-64" />
        <select className="rounded-md border border-input bg-background px-3 py-2 text-sm" value={direction}
          onChange={(e) => setDirection(e.target.value as '' | 'in' | 'out')}>
          <option value="">In & out</option>
          <option value="in">Money in</option>
          <option value="out">Money out</option>
        </select>
      </div>

      {filtered.length === 0 ? (
        <p className="rounded-lg border border-border bg-card px-4 py-6 text-sm text-muted-foreground">
          {l.entries.length === 0
            ? 'Nothing recorded yet. Payments on invoices and expenses will show up here.'
            : 'No entries match your filters.'}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border bg-card">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="px-4 py-2.5 font-medium">Date</th>
                <th className="px-2 py-2.5 font-medium" />
                <th className="px-2 py-2.5 font-medium">Who</th>
                <th className="px-2 py-2.5 font-medium">Detail</th>
                <th className="px-2 py-2.5 text-right font-medium">Amount</th>
                <th className="px-4 py-2.5 text-right font-medium">Balance</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((e) => (
                <tr key={`${e.direction}-${e.id}`} className="border-b border-border last:border-b-0 hover:bg-muted/50">
                  <td className="px-4 py-2.5 font-mono text-xs">{e.day}</td>
                  <td className="px-2 py-2.5"><DirectionBadge direction={e.direction} /></td>
                  <td className="px-2 py-2.5 font-medium">{e.counterparty}</td>
                  <td className="px-2 py-2.5 text-muted-foreground">
                    {e.invoice_id ? (
                      <Link to="/invoices/$id" params={{ id: e.invoice_id }} className="font-mono text-xs text-primary hover:underline">
                        {e.kind === 'refund' ? `refund · ${e.detail}` : e.detail || 'invoice'}
                      </Link>
                    ) : e.kind === 'expense' ? (
                      <span className="rounded-pill bg-muted px-2 py-0.5 text-xs">{expenseCategoryLabel(e.detail)}</span>
                    ) : (
                      e.detail || '—'
                    )}
                  </td>
                  <td className={`px-2 py-2.5 text-right font-medium tabular-nums ${
                    e.direction === 'in' ? 'text-success' : 'text-destructive'}`}>
                    {e.direction === 'in' ? '+' : '−'}{formatCents(e.amount_cents)}
                  </td>
                  <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                    {formatCents(e.balance_cents)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <Pagination total={l.total} offset={offset} onChange={setOffset} />
    </div>
  );
}
