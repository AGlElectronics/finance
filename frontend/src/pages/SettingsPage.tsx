import { DenConnection } from '@/components/DenConnection';

export function SettingsPage() {
  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Finance settings</h1>
        <p className="text-sm text-muted-foreground">
          Invoices are numbered FIN-0001, FIN-0002, … in EUR. Connect to Den to follow the organisation&apos;s branding.
        </p>
      </div>

      <DenConnection base="/api/den" />
    </div>
  );
}
