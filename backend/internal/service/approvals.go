package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Actions that can wait for a second person.
const (
	ActionVoid       = "invoice.void"
	ActionPayment    = "payment.record"
	ActionExpense    = "expense.create"
	ActionCreditNote = "credit_note.issue"
	ActionRefund     = "refund.record"
)

// Approval is a four-eyes request.
type Approval struct {
	ID     uuid.UUID `json:"id"`
	Action string    `json:"action"`
	// TargetID is the invoice (void, payment, credit note) or credit note
	// (refund); null for a new expense.
	TargetID *uuid.UUID `json:"target_id"`
	// TargetNumber is the invoice or credit note number, when there is one.
	TargetNumber string          `json:"target_number,omitempty"`
	AmountCents  int64           `json:"amount_cents"`
	Payload      json.RawMessage `json:"payload"`
	// Status is pending, approved or rejected.
	Status       string       `json:"status"`
	RequestedBy  audit.Actor  `json:"requested_by"`
	RequestedAt  time.Time    `json:"requested_at"`
	DecidedBy    *audit.Actor `json:"decided_by"`
	DecidedAt    *time.Time   `json:"decided_at"`
	DecisionNote string       `json:"decision_note"`
	// ResultID is what the approved request created (payment, expense,
	// credit note, refund); the invoice for a void.
	ResultID *uuid.UUID `json:"result_id"`
}

// ApprovalFilter narrows ListApprovals.
type ApprovalFilter struct {
	Status string
	Page
}

// ApprovalPage is one page of approvals.
type ApprovalPage struct {
	Approvals []Approval `json:"approvals"`
	Total     int64      `json:"total"`
	Page
}

// DecisionInput comes with an approval or rejection.
type DecisionInput struct {
	Note string `json:"note"`
}

// needsApproval: amounts above the threshold need a second person.
func (s *Service) needsApproval(amount int64) bool {
	return amount > s.opts.ApprovalThresholdCents
}

func (s *Service) requestApproval(ctx context.Context, tx pgx.Tx, who audit.Actor, action string, target *uuid.UUID, amount int64, payload any) (uuid.UUID, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO approvals (action, target_id, amount_cents, payload, requested_by, requested_issuer, requested_subject)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		action, target, amount, raw, who.Name, who.Issuer, who.Subject).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "approvals_one_pending_void") {
			return uuid.Nil, httperror.Conflict("a void of this invoice is already waiting for approval")
		}
		return uuid.Nil, err
	}
	return id, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "approval.requested", EntityType: "approval", EntityID: id,
		After: map[string]any{"action": action, "target_id": target, "amount_cents": amount, "payload": json.RawMessage(raw)},
	})
}

const approvalCols = `a.id, a.action, a.target_id, COALESCE(i.number, cn.number, ''), a.amount_cents, a.payload, a.status,
	a.requested_by, a.requested_issuer, a.requested_subject, a.requested_at,
	a.decided_by, a.decided_issuer, a.decided_subject, a.decided_at, a.decision_note, a.result_id`

const approvalFrom = ` FROM approvals a
	LEFT JOIN invoices i ON i.id = a.target_id
	LEFT JOIN credit_notes cn ON cn.id = a.target_id`

func scanApproval(row pgx.Row) (Approval, error) {
	var a Approval
	var dName, dIss, dSub *string
	err := row.Scan(&a.ID, &a.Action, &a.TargetID, &a.TargetNumber, &a.AmountCents, &a.Payload, &a.Status,
		&a.RequestedBy.Name, &a.RequestedBy.Issuer, &a.RequestedBy.Subject, &a.RequestedAt,
		&dName, &dIss, &dSub, &a.DecidedAt, &a.DecisionNote, &a.ResultID)
	if dName != nil {
		a.DecidedBy = &audit.Actor{Name: *dName, Issuer: deref(dIss), Subject: deref(dSub)}
	}
	return a, err
}

// ListApprovals returns one page of approvals, newest first.
func (s *Service) ListApprovals(ctx context.Context, f ApprovalFilter) (ApprovalPage, error) {
	switch f.Status {
	case "", "pending", "approved", "rejected":
	default:
		return ApprovalPage{}, httperror.BadRequest("status must be pending, approved or rejected")
	}
	f.Page = f.Normalize()
	out := ApprovalPage{Approvals: []Approval{}, Page: f.Page}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM approvals WHERE $1 = '' OR status = $1`, f.Status).Scan(&out.Total); err != nil {
		return out, httperror.Internal(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+approvalCols+approvalFrom+`
		 WHERE $1 = '' OR a.status = $1
		 ORDER BY a.requested_at DESC, a.id DESC LIMIT $2 OFFSET $3`, f.Status, f.Limit, f.Offset)
	if err != nil {
		return out, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return out, httperror.Internal(err)
		}
		out.Approvals = append(out.Approvals, a)
	}
	return out, rows.Err()
}

// GetApproval returns one approval.
func (s *Service) GetApproval(ctx context.Context, id uuid.UUID) (Approval, error) {
	a, err := scanApproval(s.pool.QueryRow(ctx, `SELECT `+approvalCols+approvalFrom+` WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httperror.NotFound("approval")
	}
	if err != nil {
		return a, httperror.Internal(err)
	}
	return a, nil
}

// pendingFor lists pending approvals about an invoice or its credit notes.
func (s *Service) pendingFor(ctx context.Context, invoiceID uuid.UUID) ([]Approval, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+approvalCols+approvalFrom+`
		 WHERE a.status = 'pending'
		   AND (a.target_id = $1 OR a.target_id IN (SELECT id FROM credit_notes WHERE invoice_id = $1))
		 ORDER BY a.requested_at`, invoiceID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type lockedApproval struct {
	action    string
	target    *uuid.UUID
	payload   []byte
	status    string
	requester audit.Actor
}

func lockApproval(ctx context.Context, tx pgx.Tx, id uuid.UUID) (lockedApproval, error) {
	var a lockedApproval
	err := tx.QueryRow(ctx, `
		SELECT action, target_id, payload, status, requested_by, requested_issuer, requested_subject
		  FROM approvals WHERE id = $1 FOR UPDATE`, id).
		Scan(&a.action, &a.target, &a.payload, &a.status, &a.requester.Name, &a.requester.Issuer, &a.requester.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httperror.NotFound("approval")
	}
	if err != nil {
		return a, err
	}
	if a.status != "pending" {
		return a, httperror.Conflict("this request was already " + a.status)
	}
	return a, nil
}

// Approve carries out a pending request. The approver must be a different
// person from the requester. If the request can no longer be carried out
// (the invoice was paid meanwhile, the month was closed) it stays pending
// and the error says why; reject it then.
func (s *Service) Approve(ctx context.Context, who audit.Actor, id uuid.UUID, in DecisionInput) (Approval, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		a, err := lockApproval(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.requester.SamePerson(who) {
			return httperror.New(CodeSameApprover, "you requested this yourself; a different person must approve it")
		}
		result, err := s.execApproval(ctx, tx, who, id, a)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE approvals SET status = 'approved', decided_by = $2, decided_issuer = $3, decided_subject = $4,
			       decided_at = now(), decision_note = $5, result_id = $6
			 WHERE id = $1`, id, who.Name, who.Issuer, who.Subject, strings.TrimSpace(in.Note), result); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "approval.approved", EntityType: "approval", EntityID: id,
			Before: map[string]string{"status": "pending"}, After: map[string]any{"status": "approved", "result_id": result},
			Metadata: map[string]any{"action": a.action, "requested_by": a.requester, "note": strings.TrimSpace(in.Note)},
		})
	})
	if err != nil {
		return Approval{}, err
	}
	return s.GetApproval(ctx, id)
}

func (s *Service) execApproval(ctx context.Context, tx pgx.Tx, who audit.Actor, id uuid.UUID, a lockedApproval) (uuid.UUID, error) {
	approvalID := &id
	target := uuid.Nil
	if a.target != nil {
		target = *a.target
	}
	switch a.action {
	case ActionVoid:
		var p voidPayload
		if err := json.Unmarshal(a.payload, &p); err != nil {
			return uuid.Nil, err
		}
		_, err := s.execVoid(ctx, tx, who, target, p.Reason, false, approvalID)
		return target, err
	case ActionPayment:
		var p PaymentInput
		if err := json.Unmarshal(a.payload, &p); err != nil {
			return uuid.Nil, err
		}
		return s.execPayment(ctx, tx, who, target, p, false, approvalID)
	case ActionCreditNote:
		var p CreditNoteInput
		if err := json.Unmarshal(a.payload, &p); err != nil {
			return uuid.Nil, err
		}
		_, cid, err := s.execCreditNote(ctx, tx, who, target, p, false, approvalID)
		return cid, err
	case ActionRefund:
		var p RefundInput
		if err := json.Unmarshal(a.payload, &p); err != nil {
			return uuid.Nil, err
		}
		return s.execRefund(ctx, tx, who, target, p, false, approvalID)
	case ActionExpense:
		var p ExpenseInput
		if err := json.Unmarshal(a.payload, &p); err != nil {
			return uuid.Nil, err
		}
		return s.execExpense(ctx, tx, who, p, false, approvalID)
	}
	return uuid.Nil, errors.New("unknown approval action " + a.action)
}

// Reject closes a pending request without carrying it out. The requester
// may withdraw their own request; anyone else needs the approve permission
// (checked by the route).
func (s *Service) Reject(ctx context.Context, who audit.Actor, id uuid.UUID, in DecisionInput) (Approval, error) {
	note := strings.TrimSpace(in.Note)
	if note == "" {
		return Approval{}, httperror.Validation("say why the request is rejected", map[string]string{"note": "required"})
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		a, err := lockApproval(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE approvals SET status = 'rejected', decided_by = $2, decided_issuer = $3, decided_subject = $4,
			       decided_at = now(), decision_note = $5
			 WHERE id = $1`, id, who.Name, who.Issuer, who.Subject, note); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "approval.rejected", EntityType: "approval", EntityID: id,
			Before: map[string]string{"status": "pending"}, After: map[string]string{"status": "rejected"},
			Metadata: map[string]any{"action": a.action, "requested_by": a.requester, "note": note},
		})
	})
	if err != nil {
		return Approval{}, err
	}
	return s.GetApproval(ctx, id)
}

// RequesterOf returns who asked for an approval (used by the route to let
// requesters withdraw their own requests).
func (s *Service) RequesterOf(ctx context.Context, id uuid.UUID) (audit.Actor, error) {
	var a audit.Actor
	err := s.pool.QueryRow(ctx, `SELECT requested_by, requested_issuer, requested_subject FROM approvals WHERE id = $1`, id).
		Scan(&a.Name, &a.Issuer, &a.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httperror.NotFound("approval")
	}
	if err != nil {
		return a, httperror.Internal(err)
	}
	return a, nil
}
