# Finance domain model

Outgoing invoices, credit notes, expenses and a simple money ledger. No bank
connection yet: payments, refunds and expenses are typed in by hand, and the
`method` + `reference` fields on them are the hook a later bank feed would
match transactions against.

The full guide, API reference and configuration live on the documentation
site: <https://aglelectronics.github.io/con-services/modules/finance/>.

## Money

- All amounts are **integer cents** in the installation's one currency
  (`FINANCE_CURRENCY`, default EUR).
- Line quantities are stored in **hundredths** (1.5 hours → `150`).
- Tax rates are **basis points** (21% VAT → `2100`).
- Line subtotal = quantity × unit price, rounded half away from zero to a
  whole cent; tax is applied per line. Totals are the line sums and are
  stored on the document.
- Bounds keep the arithmetic inside int64: unit price 0 to 10,000,000.00,
  quantity 0.01 to 1,000,000, line subtotal at most 1,000,000,000,000.00,
  200 lines per document. Negative prices are not allowed; corrections are
  credit notes.

## Entities

| Table | What it is |
| --- | --- |
| `customers` | Who an invoice is addressed to (name, email, billing address, tax id). Archived instead of deleted once invoices exist. Changing a customer never changes an issued invoice. |
| `invoices` | Stored status is only `draft` / `issued` / `void`. `number` is NULL until issued. Issuing freezes a copy of the customer and seller details and stores the PDF's S3 key and SHA-256; a trigger refuses later changes. |
| `invoice_lines` | Description, quantity (hundredths), unit price (cents), tax rate (bp). Rewritten wholesale while the invoice is a draft; locked after issue. |
| `payments` | Money received: date, amount, method (`bank_transfer` / `cash` / `card` / `other`), reference, note. Never changed or deleted. |
| `invoice_counter`, `credit_note_counter` | One row each, bumped inside the issue transaction so numbers (`FIN-0001`, `FIN-CN-0001`) have no gaps. |
| `credit_notes`, `credit_note_lines` | Corrections of issued invoices in their own series, with the invoice's frozen customer and seller details and their own PDF. Never changed or deleted. |
| `refunds` | Money paid back against a credit note. Never changed or deleted. |
| `expenses` | Money going out: date, vendor, category, amount, method, reference. Soft-deleted (`deleted_at`, `deleted_reason`); a trigger refuses `DELETE`. Amount and date are fixed once recorded. |
| `approvals` | Four-eyes requests waiting for a second person. |
| `period_closures` | Closed months. Nothing dated in a closed month may change. |
| `idempotency_keys` | One row per `Idempotency-Key` and person, pointing at what the request created. |
| `settings` | Finance's own seller details; empty fields fall back to Den. |
| `audit_events` | Append-only log of every change, written in the same transaction: who (name, issuer, subject), what, and the values before and after. |
| `den_link` | One row when paired with Den (see `internal/denlink`). |

## Invoice lifecycle

```
draft --issue--> issued --payment(s)--> partial --> paid
                   |                        |         |
                   +-- void (no payments) --+         |
                   |                                  |
                   +------ credit note(s) ------------+--> credited (net total 0)
                                                      +--> refund (money paid beyond the net total)
```

- **Draft**: editable (customer, dates, notes, lines). Can be deleted.
- **Issue**: takes the next number, stamps `issue_date` (default today) and
  `due_date` (default the draft's, else +30 days), freezes customer and
  seller, renders the PDF in Go and stores it in S3. If the PDF cannot be
  stored nothing is issued and no number is used.
- **Issued / partial / paid / credited**: derived at read time from
  payments, credit notes and refunds; never stored.
- **Void**: only for an issued invoice with no payments and no credit
  notes. The number stays taken. Everything else is corrected with a credit
  note (and a refund when money must go back).
- **Overdue** is a flag, not a status: an issued/partial invoice past its due
  date.

## Four-eyes approval

Voids, payments, expenses, credit notes and refunds whose amount is above
`FINANCE_APPROVAL_THRESHOLD_CENTS` (default 0, so all of them) are stored as
a pending approval. A different person with the approver role approves
(which carries the request out, re-checking everything) or rejects it; the
requester may withdraw it.

## Roles

| Role (group) | Permissions |
| --- | --- |
| `finance_read-only` | `finance.read`, `finance.audit` |
| `finance_bookkeeper` | `finance.read`, `finance.write` |
| `finance_approver` | `finance.read`, `finance.approve`, `finance.audit` |
| `finance_admin` | all of the above plus `finance.admin` |

## API

The API reference with every route, permission and `curl` example is
<https://aglelectronics.github.io/con-services/modules/finance/api/>.
