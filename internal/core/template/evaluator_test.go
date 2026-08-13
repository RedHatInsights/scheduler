package template

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func mustEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	e, err := NewEvaluator()
	if err != nil {
		t.Fatalf("NewEvaluator() error: %v", err)
	}
	return e
}

func evalDate(t *testing.T, e *Evaluator, expr string, now time.Time) string {
	t.Helper()
	ctx := map[string]any{"now": now, "job_id": "test"}
	result, err := e.EvaluateExpr(expr, ctx)
	if err != nil {
		t.Fatalf("EvaluateExpr(%q) error: %v", expr, err)
	}
	s, ok := result.(string)
	if !ok {
		t.Fatalf("EvaluateExpr(%q) returned %T, want string", expr, result)
	}
	return s
}

func evalTimestamp(t *testing.T, e *Evaluator, expr string, now time.Time) time.Time {
	t.Helper()
	ctx := map[string]any{"now": now, "job_id": "test"}
	result, err := e.EvaluateExpr(expr, ctx)
	if err != nil {
		t.Fatalf("EvaluateExpr(%q) error: %v", expr, err)
	}
	ts, ok := result.(time.Time)
	if !ok {
		t.Fatalf("EvaluateExpr(%q) returned %T, want time.Time", expr, result)
	}
	return ts
}

func TestStartOfDay(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"mid-day", time.Date(2026, 8, 12, 14, 30, 45, 0, time.UTC), "2026-08-12"},
		{"midnight", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "2026-08-12"},
		{"end of day", time.Date(2026, 8, 12, 23, 59, 59, 0, time.UTC), "2026-08-12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.start_of_day().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("start_of_day() = %s, want %s", got, tt.want)
			}
			ts := evalTimestamp(t, e, "now.start_of_day()", tt.now)
			if ts.Hour() != 0 || ts.Minute() != 0 || ts.Second() != 0 {
				t.Errorf("start_of_day() time = %v, want 00:00:00", ts)
			}
		})
	}
}

func TestEndOfDay(t *testing.T) {
	e := mustEvaluator(t)
	now := time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC)
	ts := evalTimestamp(t, e, "now.end_of_day()", now)
	if ts.Hour() != 23 || ts.Minute() != 59 || ts.Second() != 59 {
		t.Errorf("end_of_day() time = %v, want 23:59:59", ts)
	}
	if ts.Day() != 12 {
		t.Errorf("end_of_day() day = %d, want 12", ts.Day())
	}
}

func TestFirstOfMonth(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"mid-month", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "2026-08-01"},
		{"first day", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "2026-08-01"},
		{"last day", time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), "2026-08-01"},
		{"february", time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC), "2026-02-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.first_of_month().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("first_of_month() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLastOfMonth(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"august 31 days", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "2026-08-31"},
		{"february non-leap", time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC), "2026-02-28"},
		{"february leap", time.Date(2028, 2, 14, 0, 0, 0, 0, time.UTC), "2028-02-29"},
		{"april 30 days", time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), "2026-04-30"},
		{"december", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "2026-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.last_of_month().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("last_of_month() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFirstOfLastMonth(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"august -> july", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "2026-07-01"},
		{"january -> december", time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "2025-12-01"},
		{"march -> february", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), "2026-02-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.first_of_last_month().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("first_of_last_month() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLastOfLastMonth(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"august -> jul 31", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "2026-07-31"},
		{"march -> feb 28", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), "2026-02-28"},
		{"march -> feb 29 leap", time.Date(2028, 3, 15, 0, 0, 0, 0, time.UTC), "2028-02-29"},
		{"january -> dec 31", time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), "2025-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.last_of_last_month().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("last_of_last_month() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFirstOfWeek(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"wednesday", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "2026-08-10"},
		{"monday", time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), "2026-08-10"},
		{"sunday", time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), "2026-08-10"},
		{"saturday", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "2026-08-10"},
		{"crosses month boundary", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "2026-08-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.first_of_week().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("first_of_week() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLastOfWeek(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"wednesday", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "2026-08-16"},
		{"monday", time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), "2026-08-16"},
		{"sunday", time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), "2026-08-16"},
		{"crosses month boundary", time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), "2026-09-06"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.last_of_week().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("last_of_week() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFirstOfQuarter(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"Q1 jan", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), "2026-01-01"},
		{"Q1 mar", time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), "2026-01-01"},
		{"Q2 apr", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), "2026-04-01"},
		{"Q2 jun", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), "2026-04-01"},
		{"Q3 aug", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "2026-07-01"},
		{"Q4 dec", time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), "2026-10-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.first_of_quarter().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("first_of_quarter() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLastOfQuarter(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"Q1", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), "2026-03-31"},
		{"Q2", time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), "2026-06-30"},
		{"Q3", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "2026-09-30"},
		{"Q4", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), "2026-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, "now.last_of_quarter().format_date('2006-01-02')", tt.now)
			if got != tt.want {
				t.Errorf("last_of_quarter() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAddMonths(t *testing.T) {
	e := mustEvaluator(t)
	tests := []struct {
		name string
		now  time.Time
		expr string
		want string
	}{
		{"forward 1", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "now.add_months(1).format_date('2006-01-02')", "2026-09-12"},
		{"backward 1", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "now.add_months(-1).format_date('2006-01-02')", "2026-07-12"},
		{"cross year forward", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), "now.add_months(3).format_date('2006-01-02')", "2027-02-15"},
		{"cross year backward", time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), "now.add_months(-3).format_date('2006-01-02')", "2025-11-10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, tt.expr, tt.now)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestComposability(t *testing.T) {
	e := mustEvaluator(t)
	now := time.Date(2026, 8, 12, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			"last day of previous month via first_of_month - 1 day",
			"now.first_of_month().add_days(-1).format_date('2006-01-02')",
			"2026-07-31",
		},
		{
			"3 months ago first of month",
			"now.add_months(-3).first_of_month().format_date('2006-01-02')",
			"2026-05-01",
		},
		{
			"start of day of first of week",
			"now.first_of_week().start_of_day().format_date('2006-01-02T15:04:05')",
			"2026-08-10T00:00:00",
		},
		{
			"last of quarter then end of day",
			"now.last_of_quarter().end_of_day().format_date('2006-01-02T15:04:05')",
			"2026-09-30T23:59:59",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalDate(t, e, tt.expr, now)
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestExpressionLengthLimit(t *testing.T) {
	e := mustEvaluator(t)
	ctx := map[string]any{"now": time.Now(), "job_id": "test"}

	longExpr := "now.format_date('" + strings.Repeat("x", maxExprLength) + "')"
	_, err := e.EvaluateExpr(longExpr, ctx)
	if err == nil {
		t.Fatal("expected error for expression exceeding max length")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("unexpected error: %v", err)
	}

	shortExpr := "now.format_date('2006-01-02')"
	_, err = e.EvaluateExpr(shortExpr, ctx)
	if err != nil {
		t.Errorf("legitimate expression should succeed: %v", err)
	}
}

func TestEvalCostLimit(t *testing.T) {
	e := mustEvaluator(t)
	ctx := map[string]any{"now": time.Now(), "job_id": "test"}

	elements := make([]string, 50)
	for i := range elements {
		elements[i] = fmt.Sprintf("%d", i)
	}
	list := "[" + strings.Join(elements, ",") + "]"
	expr := list + ".map(x, " + list + ".map(y, x + y)).size()"

	_, err := e.EvaluateExpr(expr, ctx)
	if err == nil {
		t.Fatal("expected error for expensive comprehension expression")
	}
	if !strings.Contains(err.Error(), "cost limit") && !strings.Contains(err.Error(), "operation cancelled") {
		t.Errorf("expected cost limit error, got: %v", err)
	}
}

func TestPayloadDepthLimit(t *testing.T) {
	e := mustEvaluator(t)
	ctx := map[string]any{"now": time.Now(), "job_id": "test"}

	var nested any = "cel:job_id"
	for i := 0; i < maxPayloadDepth+5; i++ {
		nested = map[string]any{"level": nested}
	}

	_, err := e.ProcessPayload(nested, ctx)
	if err == nil {
		t.Fatal("expected error for deeply nested payload")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEvalCountLimit(t *testing.T) {
	e := mustEvaluator(t)
	ctx := map[string]any{"now": time.Now(), "job_id": "test"}

	payload := make(map[string]any, maxEvalCount+10)
	for i := 0; i < maxEvalCount+10; i++ {
		payload[fmt.Sprintf("field_%d", i)] = "cel:job_id"
	}

	_, err := e.ProcessPayload(payload, ctx)
	if err == nil {
		t.Fatal("expected error for too many CEL expressions")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLimitsAllowLegitimatePayloads(t *testing.T) {
	e := mustEvaluator(t)
	now := time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC)
	ctx := map[string]any{"now": now, "job_id": "job_123"}

	payload := map[string]any{
		"start":   "cel:now.first_of_last_month().format_date('2006-01-02')",
		"end":     "cel:now.last_of_last_month().format_date('2006-01-02')",
		"quarter": "cel:now.first_of_quarter().format_date('2006-01-02')",
		"ref":     "cel:job_id",
		"static":  "plain value",
		"nested": map[string]any{
			"week_start": "cel:now.first_of_week().format_date('2006-01-02')",
			"deep": map[string]any{
				"composed": "cel:now.first_of_month().add_days(-1).format_date('2006-01-02')",
			},
		},
		"list": []any{"cel:job_id + '_a'", "cel:job_id + '_b'"},
	}

	result, err := e.ProcessPayload(payload, ctx)
	if err != nil {
		t.Fatalf("legitimate payload should succeed: %v", err)
	}

	m := result.(map[string]any)
	if m["start"] != "2026-07-01" {
		t.Errorf("start = %v, want 2026-07-01", m["start"])
	}
	if m["ref"] != "job_123" {
		t.Errorf("ref = %v, want job_123", m["ref"])
	}
}

func TestProcessPayloadWithShortcuts(t *testing.T) {
	e := mustEvaluator(t)
	now := time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC)
	ctx := map[string]any{"now": now, "job_id": "job_123"}

	payload := map[string]any{
		"period_start": "cel:now.first_of_last_month().format_date('2006-01-02')",
		"period_end":   "cel:now.last_of_last_month().format_date('2006-01-02')",
		"static_field": "no_eval",
		"nested": map[string]any{
			"quarter_start": "cel:now.first_of_quarter().format_date('2006-01-02')",
		},
	}

	result, err := e.ProcessPayload(payload, ctx)
	if err != nil {
		t.Fatalf("ProcessPayload error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}

	expectations := map[string]string{
		"period_start": "2026-07-01",
		"period_end":   "2026-07-31",
		"static_field": "no_eval",
	}
	for key, want := range expectations {
		got, ok := m[key].(string)
		if !ok {
			t.Errorf("field %q type = %T, want string", key, m[key])
			continue
		}
		if got != want {
			t.Errorf("field %q = %s, want %s", key, got, want)
		}
	}

	nested, ok := m["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested type = %T, want map[string]any", m["nested"])
	}
	if got := nested["quarter_start"].(string); got != "2026-07-01" {
		t.Errorf("nested.quarter_start = %s, want 2026-07-01", got)
	}
}

// --- ValidatePayload tests ---

func TestValidatePayload_ValidExpressions(t *testing.T) {
	e := mustEvaluator(t)

	payload := map[string]any{
		"start":   "cel:now.first_of_last_month().format_date('2006-01-02')",
		"end":     "cel:now.last_of_last_month().format_date('2006-01-02')",
		"ref":     "cel:job_id",
		"static":  "plain value",
		"number":  42,
		"boolean": true,
		"nested": map[string]any{
			"week_start": "cel:now.first_of_week().format_date('2006-01-02')",
		},
		"list": []any{"cel:job_id + '_a'", "static_item"},
	}

	err := e.ValidatePayload(payload)
	if err != nil {
		t.Errorf("ValidatePayload() should return nil for valid payload, got: %v", err)
	}
}

func TestValidatePayload_InvalidCELExpression(t *testing.T) {
	e := mustEvaluator(t)

	payload := map[string]any{
		"bad_expr": "cel:this is not valid CEL !!@@##",
	}

	err := e.ValidatePayload(payload)
	if err == nil {
		t.Fatal("ValidatePayload() should return error for invalid CEL expression")
	}
	if !strings.Contains(err.Error(), "compile error") {
		t.Errorf("expected compile error, got: %v", err)
	}
}

func TestValidatePayload_NoCELExpressions(t *testing.T) {
	e := mustEvaluator(t)

	payload := map[string]any{
		"static_field": "hello",
		"number":       123,
		"nested": map[string]any{
			"also_static": "world",
		},
	}

	err := e.ValidatePayload(payload)
	if err != nil {
		t.Errorf("ValidatePayload() should return nil for payload with no cel: expressions, got: %v", err)
	}
}

func TestValidatePayload_ExpressionLengthLimit(t *testing.T) {
	e := mustEvaluator(t)

	longExpr := "cel:now.format_date('" + strings.Repeat("x", maxExprLength) + "')"
	payload := map[string]any{
		"too_long": longExpr,
	}

	err := e.ValidatePayload(payload)
	if err == nil {
		t.Fatal("ValidatePayload() should return error for expression exceeding max length")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected exceeds maximum error, got: %v", err)
	}
}

func TestValidatePayload_NestingDepthLimit(t *testing.T) {
	e := mustEvaluator(t)

	var nested any = "cel:job_id"
	for i := 0; i < maxPayloadDepth+5; i++ {
		nested = map[string]any{"level": nested}
	}

	err := e.ValidatePayload(nested)
	if err == nil {
		t.Fatal("ValidatePayload() should return error for deeply nested payload")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected nesting depth error, got: %v", err)
	}
}

func TestValidatePayload_EvalCountLimit(t *testing.T) {
	e := mustEvaluator(t)

	payload := make(map[string]any, maxEvalCount+10)
	for i := 0; i < maxEvalCount+10; i++ {
		payload[fmt.Sprintf("field_%d", i)] = "cel:job_id"
	}

	err := e.ValidatePayload(payload)
	if err == nil {
		t.Fatal("ValidatePayload() should return error for too many CEL expressions")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected exceeds maximum error, got: %v", err)
	}
}

func TestValidatePayload_NilPayload(t *testing.T) {
	e := mustEvaluator(t)

	err := e.ValidatePayload(nil)
	if err != nil {
		t.Errorf("ValidatePayload(nil) should return nil, got: %v", err)
	}
}

func TestValidatePayload_DoesNotEvaluate(t *testing.T) {
	e := mustEvaluator(t)

	// This expression is valid CEL but references a valid variable.
	// ValidatePayload should compile it without evaluating, so it should succeed
	// even without providing runtime context.
	payload := map[string]any{
		"expr": "cel:now.add_days(-30).format_date('2006-01-02')",
	}

	err := e.ValidatePayload(payload)
	if err != nil {
		t.Errorf("ValidatePayload() should compile without evaluating, got: %v", err)
	}
}
