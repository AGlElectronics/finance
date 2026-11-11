import type { InvoiceStatus } from '@/api/invoices';
import { cn } from '@/lib/utils';

const LOOK: Record<InvoiceStatus, { label: string; cls: string }> = {
  draft: { label: 'Draft', cls: 'bg-muted text-muted-foreground' },
  issued: { label: 'Issued', cls: 'bg-info-soft text-info-foreground' },
  partial: { label: 'Partially paid', cls: 'bg-warning-soft text-warning-foreground' },
  paid: { label: 'Paid', cls: 'bg-success-soft text-success-foreground' },
  credited: { label: 'Credited', cls: 'bg-muted text-muted-foreground' },
  void: { label: 'Void', cls: 'bg-muted text-muted-foreground line-through' },
};

export function InvoiceStatusBadge({ status, overdue }: { status: InvoiceStatus; overdue?: boolean }) {
  const look = LOOK[status] ?? LOOK.draft;
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('rounded-pill px-2 py-0.5 text-xs font-semibold', look.cls)}>{look.label}</span>
      {overdue && (
        <span className="rounded-pill bg-destructive px-2 py-0.5 text-xs font-semibold text-destructive-foreground">
          Overdue
        </span>
      )}
    </span>
  );
}
