import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  type ErrorComponentProps,
} from '@tanstack/react-router';

import { INVOICE_STATUSES, type InvoiceStatus } from '@/api/invoices';
import { Layout } from '@/components/Layout';
import { getUserManager, isOidcAuthMode } from '@/auth/oidc';
import { ApprovalsPage } from '@/pages/ApprovalsPage';
import { AuditPage } from '@/pages/AuditPage';
import { CustomersPage } from '@/pages/CustomersPage';
import { DashboardPage } from '@/pages/DashboardPage';
import { ExpensesPage } from '@/pages/ExpensesPage';
import { LedgerPage } from '@/pages/LedgerPage';
import { InvoiceDetailPage } from '@/pages/InvoiceDetailPage';
import { InvoiceEditorPage } from '@/pages/InvoiceEditorPage';
import { InvoicesPage } from '@/pages/InvoicesPage';
import { LoginPage } from '@/pages/LoginPage';
import { OidcCallbackPage } from '@/pages/OidcCallbackPage';
import { SettingsPage } from '@/pages/SettingsPage';

const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (search: Record<string, unknown>): { redirect: string } => {
    const r = search.redirect;
    if (typeof r === 'string' && r.startsWith('/')) {
      return { redirect: r };
    }
    return { redirect: '/dashboard' };
  },
  component: LoginPage,
});

const oidcCallbackRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/auth/callback',
  component: OidcCallbackPage,
});

async function requireSession() {
  if (!isOidcAuthMode()) {
    return;
  }
  const mgr = getUserManager();
  if (!mgr) {
    throw redirect({ to: '/login', search: { redirect: '/dashboard' } });
  }
  let user = await mgr.getUser();
  // Tokens are kept in memory, so after a reload there is no user yet: sign in
  // silently through the Keycloak session before sending anyone to the login page.
  if (!user || user.expired) {
    try {
      user = await mgr.signinSilent();
    } catch {
      user = null;
    }
  }
  if (!user) {
    const path =
      typeof window !== 'undefined'
        ? `${window.location.pathname}${window.location.search}`
        : '/dashboard';
    const redirectTo =
      path.startsWith('/login') || path.startsWith('/auth/') ? '/dashboard' : path;
    throw redirect({ to: '/login', search: { redirect: redirectTo } });
  }
}

const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  component: Layout,
  beforeLoad: requireSession,
});

const indexRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/dashboard' });
  },
});

const dashboardRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/dashboard',
  component: DashboardPage,
});

const customersRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/customers',
  component: CustomersPage,
});

const invoicesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/invoices',
  validateSearch: (search: Record<string, unknown>): { status?: InvoiceStatus; customer?: string } => {
    const out: { status?: InvoiceStatus; customer?: string } = {};
    if (INVOICE_STATUSES.some((s) => s.value === search.status)) {
      out.status = search.status as InvoiceStatus;
    }
    if (typeof search.customer === 'string' && search.customer) {
      out.customer = search.customer;
    }
    return out;
  },
  component: InvoicesPage,
});

const invoiceNewRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/invoices/new',
  component: InvoiceEditorPage,
});

const invoiceDetailRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/invoices/$id',
  component: InvoiceDetailPage,
});

const invoiceEditRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/invoices/$id/edit',
  component: InvoiceEditorPage,
});

const expensesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/expenses',
  component: ExpensesPage,
});

const ledgerRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/ledger',
  component: LedgerPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/settings',
  component: SettingsPage,
});

const approvalsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/approvals',
  component: ApprovalsPage,
});

const auditRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: '/audit',
  component: AuditPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  oidcCallbackRoute,
  appLayoutRoute.addChildren([
    indexRoute,
    dashboardRoute,
    customersRoute,
    invoicesRoute,
    invoiceNewRoute,
    invoiceDetailRoute,
    invoiceEditRoute,
    expensesRoute,
    ledgerRoute,
    approvalsRoute,
    auditRoute,
    settingsRoute,
  ]),
]);

function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <div className="p-6 space-y-3">
      <h1 className="text-xl font-bold">Something went wrong on this page</h1>
      <p className="text-sm text-destructive">{error instanceof Error ? error.message : String(error)}</p>
      <div className="flex gap-2">
        <button type="button" onClick={reset} className="rounded-pill bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground">
          Try again
        </button>
        <a href="/dashboard" className="rounded-pill border border-border px-4 py-2 text-sm">Back to dashboard</a>
      </div>
    </div>
  );
}

export const router = createRouter({ routeTree, defaultErrorComponent: RouteError });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
