// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// taskBasePath is the route prefix registered by routes.go for the task API.
const taskBasePath = "/api/v1/tasks"

// jsonHeader is the Content-Type header for JSON request bodies.
var jsonHeader = ut.Header{Key: "Content-Type", Value: "application/json"}

// doTaskRequest is a thin wrapper around ut.PerformRequest that sends a
// JSON body for non-GET/DELETE methods and returns the response recorder.
func doTaskRequest(engine *route.Engine, method, path string, body []byte) *ut.ResponseRecorder {
	var utBody *ut.Body
	if body != nil {
		utBody = &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	}
	return ut.PerformRequest(engine, method, path, utBody, jsonHeader)
}

// decodeAPIResponse decodes a ut response body into an api.Response.
func decodeAPIResponse(t *testing.T, w *ut.ResponseRecorder) api.Response {
	t.Helper()
	var resp api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
	}
	return resp
}

// decodeTaskData re-marshals the api.Response Data field and decodes it
// into a Task, enabling field assertions on create/get/update responses.
func decodeTaskData(t *testing.T, resp api.Response) Task {
	t.Helper()
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var r Task
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	return r
}

// createTaskViaAPI issues a POST to create a task and returns the decoded
// response. It fails the test on a non-200 response.
func createTaskViaAPI(t *testing.T, engine *route.Engine, body string) Task {
	t.Helper()
	w := doTaskRequest(engine, "POST", taskBasePath, []byte(body))
	if w.Code != http.StatusOK {
		t.Fatalf("create: status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	return decodeTaskData(t, resp)
}

// itoa converts an int64 to its decimal string representation.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// newTaskTestEngine creates a fresh route.Engine with the task CRUD routes
// wired to a handler backed by an in-memory Service. When jwtMW is
// non-nil it is mounted on the task route group, mirroring the production
// wiring where PUT /api/v1/tasks/:id requires JWT authentication.
func newTaskTestEngine(t *testing.T, jwtMW app.HandlerFunc) *route.Engine {
	t.Helper()
	h := NewHandler(NewMemoryTaskService())
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.GET(taskBasePath, h.ListTasks)
	engine.GET(taskBasePath+"/:id", h.GetTask)
	engine.POST(taskBasePath, h.CreateTask)
	if jwtMW != nil {
		engine.PUT(taskBasePath+"/:id", jwtMW, h.UpdateTask)
	} else {
		engine.PUT(taskBasePath+"/:id", h.UpdateTask)
	}
	engine.DELETE(taskBasePath+"/:id", h.DeleteTask)
	return engine
}

// stubJWTAuth is a minimal JWT-like middleware for testing: it rejects
// requests without a Bearer Authorization header, simulating the real
// JWT middleware behavior without requiring a jwt.JWT manager. This keeps
// the handler test free of pkg/auth/jwt imports.
func stubJWTAuth(ctx context.Context, arc *app.RequestContext) {
	authHeader := string(arc.GetHeader("Authorization"))
	if authHeader == "" {
		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing authorization header")
		arc.Abort()
		return
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid authorization header format")
		arc.Abort()
		return
	}
	arc.Next(ctx)
}

// --- In-memory Service for tests ---

// memoryTaskService is an in-memory Service implementation used for
// handler tests.
type memoryTaskService struct {
	mu     sync.Mutex
	tasks  map[int64]*Task
	nextID int64
}

// NewMemoryTaskService creates an in-memory Service suitable for tests.
func NewMemoryTaskService() Service {
	return &memoryTaskService{tasks: make(map[int64]*Task)}
}

func (s *memoryTaskService) ListTasks(_ context.Context, page, size int, filter Filter) ([]Task, int64, error) {
	page, size = httputil.ClampPaging(page, size)
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []Task
	for _, t := range s.tasks {
		if filter.Group != "" && t.Group != filter.Group {
			continue
		}
		result = append(result, *t)
	}
	total := int64(len(result))
	start := (page - 1) * size
	if start >= len(result) {
		return []Task{}, total, nil
	}
	end := start + size
	if end > len(result) {
		end = len(result)
	}
	return result[start:end], total, nil
}

func (s *memoryTaskService) GetTask(_ context.Context, id int64) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound()
	}
	cp := *t
	return &cp, nil
}

func (s *memoryTaskService) CreateTask(_ context.Context, req *Task) (*Task, error) {
	if req == nil {
		return nil, errBadRequest()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	now := time.Now()
	t := *req
	t.ID = s.nextID
	t.CreatedAt = now
	t.UpdatedAt = now
	s.tasks[t.ID] = &t
	cp := t
	return &cp, nil
}

func (s *memoryTaskService) UpdateTask(_ context.Context, id int64, req *Task) (*Task, error) {
	if req == nil {
		return nil, errBadRequest()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound()
	}
	req.ID = id
	req.CreatedAt = existing.CreatedAt
	req.UpdatedAt = time.Now()
	*existing = *req
	cp := *existing
	return &cp, nil
}

func (s *memoryTaskService) DeleteTask(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return errNotFound()
	}
	delete(s.tasks, id)
	return nil
}

func (s *memoryTaskService) TriggerTask(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return errNotFound()
	}
	return nil
}

func (s *memoryTaskService) PauseTask(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return errNotFound()
	}
	return nil
}

func (s *memoryTaskService) ResumeTask(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return errNotFound()
	}
	return nil
}

func (s *memoryTaskService) ListExecutions(_ context.Context, taskID int64, page, size int, _ ExecutionFilter) ([]Execution, int64, error) {
	return []Execution{}, 0, nil
}

func (s *memoryTaskService) GetExecution(_ context.Context, _, _ int64) (*Execution, error) {
	return nil, errNotFound()
}

func (s *memoryTaskService) CopyTask(_ context.Context, id int64, newName string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.tasks[id]
	if !ok {
		return nil, errNotFound()
	}
	s.nextID++
	now := time.Now()
	cp := *src
	cp.ID = s.nextID
	if newName == "" {
		cp.Name = src.Name + " (copy)"
	} else {
		cp.Name = newName
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	s.tasks[cp.ID] = &cp
	return &cp, nil
}

func (s *memoryTaskService) GetExecutionStats(_ context.Context, from, to time.Time) (ExecutionStats, error) {
	return ExecutionStats{}, nil
}

// errNotFound wraps the handler-layer sentinel so api.Fail maps it to 404.
func errNotFound() error {
	return handlerError{status: http.StatusNotFound, code: errdefs.CodeNotFound, msg: "task not found"}
}

// errBadRequest wraps the handler-layer sentinel so api.Fail maps it to 400.
func errBadRequest() error {
	return handlerError{status: http.StatusBadRequest, code: errdefs.CodeBadRequest, msg: "invalid request"}
}

// handlerError is a test-local error type implementing errdefs.ErrorCoder.
type handlerError struct {
	status int
	code   int
	msg    string
}

func (e handlerError) Error() string   { return e.msg }
func (e handlerError) HTTPStatus() int { return e.status }
func (e handlerError) Code() int       { return e.code }

// --- UpdateTask: config field update tests (preserved behavior) ---

// TestUpdateTaskConfigFields verifies that PUT /api/v1/tasks/:id successfully
// updates configuration fields.
func TestUpdateTaskConfigFields(t *testing.T) {
	engine := newTaskTestEngine(t, nil)
	created := createTaskViaAPI(t, engine, `{"name":"task-1","executor":"http","schedule":"*/1 * * * *","enabled":true}`)

	w := doTaskRequest(engine, "PUT", taskBasePath+"/"+itoa(created.ID),
		[]byte(`{"name":"task-1-updated","executor":"tcp","schedule":"*/5 * * * *","enabled":false,"description":"updated desc","group":"backup","tags":["critical"]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	r := decodeTaskData(t, resp)
	if r.ID != created.ID {
		t.Errorf("ID = %d, want %d (preserved)", r.ID, created.ID)
	}
	if r.Name != "task-1-updated" {
		t.Errorf("Name = %q, want %q", r.Name, "task-1-updated")
	}
	if r.Executor != "tcp" {
		t.Errorf("Executor = %q, want %q", r.Executor, "tcp")
	}
	if r.Schedule != "*/5 * * * *" {
		t.Errorf("Schedule = %q, want %q", r.Schedule, "*/5 * * * *")
	}
	if r.Enabled != false {
		t.Errorf("Enabled = %v, want false", r.Enabled)
	}
	if r.Description != "updated desc" {
		t.Errorf("Description = %q, want %q", r.Description, "updated desc")
	}
	if r.Group != "backup" {
		t.Errorf("Group = %q, want %q", r.Group, "backup")
	}
	if len(r.Tags) != 1 || r.Tags[0] != "critical" {
		t.Errorf("Tags = %v, want [critical]", r.Tags)
	}
}

// TestUpdateTaskNotFound verifies PUT on a non-existent ID returns 404.
func TestUpdateTaskNotFound(t *testing.T) {
	engine := newTaskTestEngine(t, nil)

	w := doTaskRequest(engine, "PUT", taskBasePath+"/999", []byte(`{"name":"x","executor":"http"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusNotFound, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeNotFound {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeNotFound)
	}
}

// TestUpdateTaskInvalidID verifies PUT /abc returns 400.
func TestUpdateTaskInvalidID(t *testing.T) {
	engine := newTaskTestEngine(t, nil)

	w := doTaskRequest(engine, "PUT", taskBasePath+"/abc", []byte(`{"name":"x","executor":"http"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// --- UpdateTask: JWT authentication tests ---

// TestUpdateTaskAPIKeyOnlyReturns401 verifies that a request carrying only
// an API key (X-Tickraft-API-Key header) but no JWT Authorization header is rejected
// with 401 Unauthorized. PUT /api/v1/tasks/:id requires JWT authentication;
// APIKey authentication is not accepted on this route.
func TestUpdateTaskAPIKeyOnlyReturns401(t *testing.T) {
	engine := newTaskTestEngine(t, stubJWTAuth)
	created := createTaskViaAPI(t, engine, `{"name":"task-1","executor":"http","schedule":"*/1 * * * *","enabled":true}`)

	body := []byte(`{"name":"task-1-updated","executor":"tcp"}`)
	utBody := &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	w := ut.PerformRequest(engine, "PUT", taskBasePath+"/"+itoa(created.ID), utBody,
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "X-Tickraft-API-Key", Value: "tk_test_api_key_12345"},
	)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeUnauthorized {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeUnauthorized)
	}
}

// TestUpdateTaskNoAuthReturns401 verifies that a request with no
// authentication credentials at all is rejected with 401.
func TestUpdateTaskNoAuthReturns401(t *testing.T) {
	engine := newTaskTestEngine(t, stubJWTAuth)
	created := createTaskViaAPI(t, engine, `{"name":"task-1","executor":"http","schedule":"*/1 * * * *","enabled":true}`)

	w := doTaskRequest(engine, "PUT", taskBasePath+"/"+itoa(created.ID),
		[]byte(`{"name":"task-1-updated","executor":"tcp"}`))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != errdefs.CodeUnauthorized {
		t.Errorf("code = %d, want %d", resp.Code, errdefs.CodeUnauthorized)
	}
}

// TestUpdateTaskWithJWTAccepted verifies that a request with a valid Bearer
// JWT token passes the JWT middleware and reaches the handler.
func TestUpdateTaskWithJWTAccepted(t *testing.T) {
	engine := newTaskTestEngine(t, stubJWTAuth)
	created := createTaskViaAPI(t, engine, `{"name":"task-1","executor":"http","schedule":"*/1 * * * *","enabled":true}`)

	body := []byte(`{"name":"task-1-updated","executor":"tcp"}`)
	utBody := &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	w := ut.PerformRequest(engine, "PUT", taskBasePath+"/"+itoa(created.ID), utBody,
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "Authorization", Value: "Bearer fake.jwt.token"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	r := decodeTaskData(t, resp)
	if r.Name != "task-1-updated" {
		t.Errorf("Name = %q, want %q", r.Name, "task-1-updated")
	}
}
