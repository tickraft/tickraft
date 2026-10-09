// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

// Package expr exposes the expression validation endpoint shared by the
// rule editors: alert rule expressions, remediation trigger conditions,
// and execution judgments.
package expr

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
)

// Environment identifiers accepted by the env field of the validate
// request. Each maps to the domain env whose ExampleEnv is the validation
// baseline.
const (
	envAlert       = "alert"
	envRemediation = "remediation"
	envExecution   = "execution"
)

// Handler exposes the expression validation endpoint. Validation is a
// pure function of the request, so the handler is stateless and needs no
// injected service; the underlying kernel (pkg/expr) is shared with the
// CRUD entry-point validations, keeping both ends the same source.
type Handler struct{}

// NewHandler creates a new expr Handler.
func NewHandler() *Handler { return &Handler{} }

// ValidateRequest is the request body of POST /api/v1/expr/validate.
type ValidateRequest struct {
	// Env selects the evaluation environment contract: alert,
	// remediation, or execution.
	Env string `json:"env"`
	// Expression is the expr-lang predicate to validate.
	Expression string `json:"expression"`
}

// ValidateResponse is the success payload of POST /api/v1/expr/validate.
type ValidateResponse struct {
	// Valid reports that the expression compiles and evaluates cleanly
	// against the selected env's example.
	Valid bool `json:"valid"`
}

// Validate handles POST /api/v1/expr/validate. It compiles the expression
// against the selected env contract and runs it once against the env's
// ExampleEnv, catching syntax errors, unknown variables, domain-field
// typos, and type mismatches. Validation is side-effect free; nothing is
// persisted. An empty expression is valid for the optional envs
// (remediation trigger condition, execution judgment — both mean
// "protocol/default semantics") and rejected for alert rules, matching
// the CRUD entry points.
func (h *Handler) Validate(_ context.Context, arc *app.RequestContext) {
	var req ValidateRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}

	var err error
	switch req.Env {
	case envAlert:
		err = alert.ValidateExpression(req.Expression)
	case envRemediation:
		if req.Expression != "" {
			err = remediation.ValidateExpression(req.Expression)
		}
	case envExecution:
		if req.Expression != "" {
			err = executor.ValidateExpression(req.Expression)
		}
	default:
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"env must be one of: alert, remediation, execution")
		return
	}
	if err != nil {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, diagnosticMessage(err))
		return
	}
	httputil.Success(arc, ValidateResponse{Valid: true})
}

// diagnosticMessage picks the message to display from a wrapped error
// chain. The kernel prefix-wraps its sentinels ("expression compile
// failed: <detail>"), so blindly unwrapping reaches a sentinel whose
// message carries no detail. The diagnostic — including the expr-lang
// line:column position the frontend renders inline — lives in the
// longest message of the chain.
func diagnosticMessage(err error) string {
	msg := err.Error()
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return msg
		}
		err = unwrapped
		if detail := err.Error(); len(detail) > len(msg) {
			msg = detail
		}
	}
}
