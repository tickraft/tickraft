// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package contact

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/quota"
)

// ErrQuotaExceeded is returned by CreateContact once the directory has
// reached the enforced contact ceiling (quota.TypeContact). The HTTP
// layer maps it to 409 Conflict, matching the asset quota contract.
var ErrQuotaExceeded = errors.New("contact: quota exceeded")

// maxNameLen bounds the name and remark columns.
const maxNameLen = 255

// maxPhoneLen bounds the phone column.
const maxPhoneLen = 64

// ContactService implements Service on top of the contact store. The
// <Domain>Service name mirrors the convention of the other domain service
// implementations (see pkg/prism/channel).
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type ContactService struct {
	contacts *Store
	logger   *zap.Logger
}

var _ Service = (*ContactService)(nil)

// NewContactService creates a ContactService backed by the given store.
func NewContactService(contacts *Store, opts ...ServiceOption) *ContactService {
	s := &ContactService{contacts: contacts, logger: zap.NewNop()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ServiceOption customizes a ContactService at construction time.
type ServiceOption func(*ContactService)

// WithLogger sets the structured logger used for audit entries.
func WithLogger(logger *zap.Logger) ServiceOption {
	return func(s *ContactService) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// ListContacts returns a page of contacts matching the filter together
// with the total count.
func (s *ContactService) ListContacts(ctx context.Context, filter ListFilter) ([]Contact, int64, error) {
	return s.contacts.List(ctx, filter)
}

// GetContact returns a single contact.
func (s *ContactService) GetContact(ctx context.Context, id int64) (*Contact, error) {
	return s.contacts.Get(ctx, id)
}

// CreateContact validates the request, enforces the contact seat ceiling,
// and persists the new contact.
func (s *ContactService) CreateContact(ctx context.Context, req *CreateRequest) (*Contact, error) {
	if req == nil {
		return nil, &ErrValidation{Msg: "request body is required"}
	}
	c, err := contactOf(req.Name, req.Email, req.Phone, req.Remark)
	if err != nil {
		return nil, err
	}
	if err := s.enforceSeat(ctx); err != nil {
		return nil, err
	}
	if err := s.contacts.Create(ctx, c); err != nil {
		return nil, err
	}
	s.logger.Info("contact created",
		zap.String("operation", "contact.create"),
		zap.String("outcome", "success"),
		zap.Int64("id", c.ID),
		zap.String("name", c.Name),
	)
	return c, nil
}

// UpdateContact applies a full update to the contact identified by id.
func (s *ContactService) UpdateContact(ctx context.Context, id int64, req *UpdateRequest) (*Contact, error) {
	if req == nil {
		return nil, &ErrValidation{Msg: "request body is required"}
	}
	c, err := contactOf(req.Name, req.Email, req.Phone, req.Remark)
	if err != nil {
		return nil, err
	}
	c.ID = id
	if err := s.contacts.Update(ctx, c); err != nil {
		return nil, err
	}
	s.logger.Info("contact updated",
		zap.String("operation", "contact.update"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
	)
	return s.contacts.Get(ctx, id)
}

// DeleteContact removes the contact identified by id.
func (s *ContactService) DeleteContact(ctx context.Context, id int64) error {
	if err := s.contacts.Delete(ctx, id); err != nil {
		return err
	}
	s.logger.Info("contact deleted",
		zap.String("operation", "contact.delete"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
	)
	return nil
}

// CountContacts returns the number of contacts in the directory.
func (s *ContactService) CountContacts(ctx context.Context) (int64, error) {
	return s.contacts.Count(ctx)
}

// enforceSeat rejects the operation when the directory is at the active
// contact ceiling. A ceiling of 0 or less means unlimited (the provider
// contract: 0 = not configured).
func (s *ContactService) enforceSeat(ctx context.Context) error {
	ceiling := quota.Ceiling(quota.TypeContact)
	if ceiling <= 0 {
		return nil
	}
	count, err := s.contacts.Count(ctx)
	if err != nil {
		return err
	}
	if count >= int64(ceiling) {
		s.logger.Warn("contact create rejected: quota exceeded",
			zap.String("operation", "contact.create"),
			zap.String("outcome", "quota_exceeded"),
			zap.Int64("current_count", count),
			zap.Int("quota", ceiling),
		)
		return ErrQuotaExceeded
	}
	return nil
}

// ErrValidation marks a payload that fails validation. The message is
// safe to return to the client verbatim.
type ErrValidation struct{ Msg string }

func (e *ErrValidation) Error() string { return "contact: " + e.Msg }

// contactOf validates the directory fields and assembles the persistence
// model. At least one of email and phone must be present; a present email
// must parse as an address.
func contactOf(name, email, phone, remark string) (*Contact, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &ErrValidation{"name is required"}
	}
	if len(name) > maxNameLen {
		return nil, &ErrValidation{"name exceeds maximum length of 255 characters"}
	}
	email = strings.TrimSpace(email)
	if len(email) > maxNameLen {
		return nil, &ErrValidation{"email exceeds maximum length of 255 characters"}
	}
	if email != "" {
		addr, err := mail.ParseAddress(email)
		if err != nil || addr.Address != email {
			return nil, &ErrValidation{"email is not a valid address"}
		}
	}
	phone = strings.TrimSpace(phone)
	if len(phone) > maxPhoneLen {
		return nil, &ErrValidation{"phone exceeds maximum length of 64 characters"}
	}
	if email == "" && phone == "" {
		return nil, &ErrValidation{"at least one of email and phone is required"}
	}
	remark = strings.TrimSpace(remark)
	if len(remark) > maxNameLen {
		return nil, &ErrValidation{"remark exceeds maximum length of 255 characters"}
	}
	return &Contact{Name: name, Email: email, Phone: phone, Remark: remark}, nil
}
