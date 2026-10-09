// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// testEnv mirrors the shape of the kernel's consumer envs: top-level
// scalars as tagged struct fields, closed domain objects as
// expr.DomainMap, open data maps as plain map types.
type testEnv struct {
	Name    string             `expr:"name"`
	Code    int                `expr:"code"`
	Metrics map[string]float64 `expr:"metrics"`
	Asset   DomainMap          `expr:"asset"`
}

// exampleTestEnv returns a fully populated sample used for compile
// contracts and sample evaluation.
func exampleTestEnv() testEnv {
	return testEnv{
		Name:    "web-1",
		Code:    200,
		Metrics: map[string]float64{"cpu": 91, "mem": 76},
		Asset: DomainMap{
			"id":   int64(42),
			"name": "web-1",
			"tags": map[string]string{"env": "prod"},
		},
	}
}

func TestCompilePredicateSucceeds(t *testing.T) {
	c := NewCompiler()
	env := exampleTestEnv()
	p, err := c.Compile(env, `metrics["cpu"] > 90 && asset.tags["env"] == "prod"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := RunBool(p, env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got {
		t.Fatal("expression should evaluate to true")
	}
}

func TestCompileAsBoolRejectsNonBool(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(exampleTestEnv(), `code + 1`)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("want ErrCompileFailed, got %v", err)
	}
}

func TestCompileMaxNodesRejectsOversized(t *testing.T) {
	c := NewCompiler()
	// "1+1+...+1" with n ones has 2n-1 AST nodes; n=502 crosses the
	// 1000-node budget.
	expression := strings.Repeat("1+", 501) + "1"
	_, err := c.Compile(exampleTestEnv(), expression)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("want ErrCompileFailed, got %v", err)
	}
}

func TestCompileUnknownTopLevelVariable(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(exampleTestEnv(), `unknown == 1`)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("want ErrCompileFailed, got %v", err)
	}
}

func TestCompileTypeMismatch(t *testing.T) {
	c := NewCompiler()
	_, err := c.Compile(exampleTestEnv(), `name > 10`)
	if !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("want ErrCompileFailed, got %v", err)
	}
}

func TestCompileNilEnvRejected(t *testing.T) {
	c := NewCompiler()
	var nilEnv *testEnv
	if _, err := c.Compile(nilEnv, `code == 200`); !errors.Is(err, ErrNilEnv) {
		t.Fatalf("want ErrNilEnv, got %v", err)
	}
}

// TestBuiltinsUnrestricted samples that the grammar-level string
// operators (matches/contains/startsWith) and standard builtins
// (fromJSON, len) work without any whitelist configuration.
func TestBuiltinsUnrestricted(t *testing.T) {
	c := NewCompiler()
	env := exampleTestEnv()
	expression := `name matches "^we." && name contains "b" && name startsWith "w" ` +
		`&& len(name) > 1 && fromJSON("1") == 1`
	p, err := c.Compile(env, expression)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := RunBool(p, env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got {
		t.Fatal("expression should evaluate to true")
	}
}

func TestCompileSubAllowsNonBool(t *testing.T) {
	c := NewCompiler()
	env := exampleTestEnv()
	p, err := c.CompileSub(env, `code + 1`)
	if err != nil {
		t.Fatalf("compile sub: %v", err)
	}
	out, err := Run(p, env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, ok := out.(int); !ok || got != 201 {
		t.Fatalf("want 201, got %#v", out)
	}
	if _, err := RunBool(p, env); !errors.Is(err, ErrNotBool) {
		t.Fatalf("want ErrNotBool from RunBool on non-bool program, got %v", err)
	}
}

func TestRunEvalFailureWrapsSentinel(t *testing.T) {
	c := NewCompiler()
	// Map field types are any at compile time, so an ordered comparison
	// against the wrong type only fails at runtime.
	p, err := c.Compile(exampleTestEnv(), `asset.id > "not-an-int"`)
	if err != nil {
		t.Fatalf("compile should accept map-backed field access: %v", err)
	}
	if _, err := RunBool(p, exampleTestEnv()); !errors.Is(err, ErrEvalFailed) {
		t.Fatalf("want ErrEvalFailed, got %v", err)
	}
}

func TestDomainMapDualAccess(t *testing.T) {
	c := NewCompiler()
	env := exampleTestEnv()
	dotted, err := RunBool(mustCompile(t, c, env, `asset.id == 42`), env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	indexed, err := RunBool(mustCompile(t, c, env, `asset["id"] == 42`), env)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !dotted || !indexed {
		t.Fatalf("asset.id and asset[\"id\"] must both work: %v / %v", dotted, indexed)
	}
}

func mustCompile(t *testing.T, c *Compiler, env testEnv, expression string) *Program {
	t.Helper()
	p, err := c.Compile(env, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	return p
}

func TestValidateCatchesErrorClasses(t *testing.T) {
	cases := []struct {
		name       string
		expression string
		wantErr    error
	}{
		{"syntax", `metrics["cpu" >`, ErrCompileFailed},
		{"unknown top-level", `unknown == 1`, ErrCompileFailed},
		{"domain field typo", `asset.naem == "x"`, ErrCompileFailed},
		{"domain field typo bracket form", `asset["naem"] == "x"`, ErrCompileFailed},
		{"domain field type error", `asset.id > "not-an-int"`, ErrEvalFailed},
		{"type mismatch", `name > 10`, ErrCompileFailed},
		{"non-bool", `code + 1`, ErrCompileFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(exampleTestEnv(), tc.expression)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateAcceptsOpenMapKeys(t *testing.T) {
	// metrics and asset.tags are open data maps: arbitrary keys are
	// valid even when absent from the sample.
	if err := Validate(exampleTestEnv(), `metrics["disk"] > 50`); err != nil {
		t.Fatalf("open map key should not be validated against the sample: %v", err)
	}
	if err := Validate(exampleTestEnv(), `asset.tags["region"] == "cn"`); err != nil {
		t.Fatalf("open tag key should not be validated against the sample: %v", err)
	}
}

func TestValidateAcceptsValidExpression(t *testing.T) {
	if err := Validate(exampleTestEnv(), `code == 200 && metrics["cpu"] > 90`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProgramCacheHitReusesPointer(t *testing.T) {
	cache := NewProgramCache(4)
	env := exampleTestEnv()
	a, err := cache.Compile(env, `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	b, err := cache.Compile(env, `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if a != b {
		t.Fatal("cache hit should return the same program pointer")
	}
	if cache.Len() != 1 {
		t.Fatalf("want 1 cached program, got %d", cache.Len())
	}
}

func TestProgramCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := NewProgramCache(2)
	env := exampleTestEnv()
	first, err := cache.Compile(env, `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := cache.Compile(env, `code == 201`); err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Touch "first" so "code == 201" becomes the LRU entry.
	if _, err := cache.Compile(env, `code == 200`); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := cache.Compile(env, `code == 202`); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if cache.Len() != 2 {
		t.Fatalf("want 2 cached programs after eviction, got %d", cache.Len())
	}
	again, err := cache.Compile(env, `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if again != first {
		t.Fatal("recently used program should have survived eviction")
	}
}

func TestProgramCacheKeysByEnvType(t *testing.T) {
	cache := NewProgramCache(4)
	type otherEnv struct {
		Code int `expr:"code"`
	}
	a, err := cache.Compile(exampleTestEnv(), `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	b, err := cache.Compile(otherEnv{Code: 200}, `code == 200`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if a == b {
		t.Fatal("different env types must not share a cache entry")
	}
	if cache.Len() != 2 {
		t.Fatalf("want 2 cached programs, got %d", cache.Len())
	}
}

func TestProgramCacheEval(t *testing.T) {
	cache := NewProgramCache(4)
	env := exampleTestEnv()
	got, err := cache.Eval(`metrics["cpu"] > 95`, env)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got {
		t.Fatal("cpu=91 should not exceed 95")
	}
	if _, err := cache.Eval(`bogus ~~`, env); !errors.Is(err, ErrCompileFailed) {
		t.Fatalf("want ErrCompileFailed, got %v", err)
	}
}

// TestProgramCacheConcurrent exercises concurrent Compile/Eval under
// the race detector (go test -race).
func TestProgramCacheConcurrent(t *testing.T) {
	cache := NewProgramCache(8)
	env := exampleTestEnv()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			expression := `code == ` + string(rune('0'+i%3+2)) + `00` // 200..400
			if _, err := cache.Compile(env, expression); err != nil {
				t.Errorf("compile %q: %v", expression, err)
			}
			if _, err := cache.Eval(`code > 0`, env); err != nil {
				t.Errorf("eval: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if cache.Len() > 8 {
		t.Fatalf("cache exceeded capacity: %d", cache.Len())
	}
}
