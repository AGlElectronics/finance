package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// AuditFilter narrows AuditLog. Empty fields match everything; Action
// matches exactly or as a prefix ("invoice" finds invoice.issued).
type AuditFilter struct {
	EntityType string
	EntityID   *uuid.UUID
	Action     string
	Actor      string
	Page
}

// AuditPage is one page of the audit log.
type AuditPage struct {
	Events []audit.Entry `json:"events"`
	Total  int64         `json:"total"`
	Page
}

// AuditLog returns one page of audit events, newest first.
func (s *Service) AuditLog(ctx context.Context, f AuditFilter) (AuditPage, error) {
	f.Page = f.Normalize()
	events, total, err := s.audit.List(ctx, audit.Filter{
		EntityType: f.EntityType, EntityID: f.EntityID, Action: f.Action, Actor: f.Actor,
		Limit: f.Limit, Offset: f.Offset,
	})
	if err != nil {
		return AuditPage{}, httperror.Internal(err)
	}
	return AuditPage{Events: events, Total: total, Page: f.Page}, nil
}
