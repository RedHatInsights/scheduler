package template

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const celPrefix = "scheduler_cel:"

const (
	maxExprLength   = 1024
	maxEvalCost     = 10_000
	maxPayloadDepth = 20
	maxEvalCount    = 50
)

var dateFormatConstants = map[string]string{
	"ISO_DATE":      "2006-01-02",
	"ISO_DATETIME":  "2006-01-02T15:04:05Z",
	"ISO_8601":      "2006-01-02T15:04:05Z07:00",
	"US_DATE":       "01/02/2006",
	"EU_DATE":       "02/01/2006",
	"DATE_SLASH":    "2006/01/02",
	"YEAR_MONTH":    "2006-01",
	"MONTH_DAY":     "01-02",
	"DATETIME_FULL": "2006-01-02 15:04:05",
}

// addMonthsClamped adds n months to t, clamping the day to the last day of the
// target month when it would otherwise overflow (e.g. Jan 31 + 1 month = Feb 28).
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	targetMonth := time.Month(int(m) + n)
	lastDay := time.Date(y, targetMonth+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > lastDay {
		d = lastDay
	}
	return time.Date(y, targetMonth, d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// Evaluator manages the CEL environment and executes templated expressions.
type Evaluator struct {
	env *cel.Env
}

// NewEvaluator sets up the CEL environment with context variables and custom date functions.
func NewEvaluator() (*Evaluator, error) {
	envOpts := []cel.EnvOption{
		cel.Variable("now", cel.TimestampType),
		cel.Variable("job_id", cel.StringType),
	}

	for name := range dateFormatConstants {
		envOpts = append(envOpts, cel.Variable(name, cel.StringType))
	}

	envOpts = append(envOpts,

		// Custom Member Function 1: timestamp.add_days(int) -> timestamp
		cel.Function("add_days",
			cel.MemberOverload("timestamp_add_days_int",
				[]*cel.Type{cel.TimestampType, cel.IntType},
				cel.TimestampType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					ts, ok := lhs.(types.Timestamp)
					if !ok {
						return types.ValOrErr(lhs, "lhs must be a timestamp")
					}
					days, ok := rhs.(types.Int)
					if !ok {
						return types.ValOrErr(rhs, "rhs must be an int")
					}
					// Add calendar days handling leap years & DST boundaries
					newTime := ts.Time.AddDate(0, 0, int(days))
					return types.Timestamp{Time: newTime}
				}),
			),
		),

		// timestamp.add_months(int) -> timestamp
		cel.Function("add_months",
			cel.MemberOverload("timestamp_add_months_int",
				[]*cel.Type{cel.TimestampType, cel.IntType},
				cel.TimestampType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					ts, ok := lhs.(types.Timestamp)
					if !ok {
						return types.ValOrErr(lhs, "lhs must be a timestamp")
					}
					months, ok := rhs.(types.Int)
					if !ok {
						return types.ValOrErr(rhs, "rhs must be an int")
					}
					newTime := addMonthsClamped(ts.Time, int(months))
					return types.Timestamp{Time: newTime}
				}),
			),
		),

		// timestamp.start_of_day() -> timestamp
		cel.Function("start_of_day",
			cel.MemberOverload("timestamp_start_of_day",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					return types.Timestamp{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
				}),
			),
		),

		// timestamp.end_of_day() -> timestamp
		cel.Function("end_of_day",
			cel.MemberOverload("timestamp_end_of_day",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					return types.Timestamp{Time: time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)}
				}),
			),
		),

		// timestamp.first_of_month() -> timestamp
		cel.Function("first_of_month",
			cel.MemberOverload("timestamp_first_of_month",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					return types.Timestamp{Time: time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)}
				}),
			),
		),

		// timestamp.last_of_month() -> timestamp
		cel.Function("last_of_month",
			cel.MemberOverload("timestamp_last_of_month",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					last := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC)
					return types.Timestamp{Time: last}
				}),
			),
		),

		// timestamp.first_of_last_month() -> timestamp
		cel.Function("first_of_last_month",
			cel.MemberOverload("timestamp_first_of_last_month",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					first := time.Date(t.Year(), t.Month()-1, 1, 0, 0, 0, 0, time.UTC)
					return types.Timestamp{Time: first}
				}),
			),
		),

		// timestamp.last_of_last_month() -> timestamp
		cel.Function("last_of_last_month",
			cel.MemberOverload("timestamp_last_of_last_month",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					last := time.Date(t.Year(), t.Month(), 0, 0, 0, 0, 0, time.UTC)
					return types.Timestamp{Time: last}
				}),
			),
		),

		// timestamp.first_of_week() -> timestamp (Monday-based, ISO 8601)
		cel.Function("first_of_week",
			cel.MemberOverload("timestamp_first_of_week",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					weekday := int(t.Weekday())
					if weekday == 0 {
						weekday = 7
					}
					monday := t.AddDate(0, 0, -(weekday - 1))
					return types.Timestamp{Time: time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)}
				}),
			),
		),

		// timestamp.last_of_week() -> timestamp (Sunday, end of ISO week)
		cel.Function("last_of_week",
			cel.MemberOverload("timestamp_last_of_week",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					weekday := int(t.Weekday())
					if weekday == 0 {
						weekday = 7
					}
					sunday := t.AddDate(0, 0, 7-weekday)
					return types.Timestamp{Time: time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 0, 0, 0, 0, time.UTC)}
				}),
			),
		),

		// timestamp.first_of_quarter() -> timestamp
		cel.Function("first_of_quarter",
			cel.MemberOverload("timestamp_first_of_quarter",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					qMonth := ((t.Month() - 1) / 3 * 3) + 1
					return types.Timestamp{Time: time.Date(t.Year(), qMonth, 1, 0, 0, 0, 0, time.UTC)}
				}),
			),
		),

		// timestamp.last_of_quarter() -> timestamp
		cel.Function("last_of_quarter",
			cel.MemberOverload("timestamp_last_of_quarter",
				[]*cel.Type{cel.TimestampType},
				cel.TimestampType,
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					ts, ok := val.(types.Timestamp)
					if !ok {
						return types.ValOrErr(val, "expected timestamp")
					}
					t := ts.Time.UTC()
					qEnd := ((t.Month()-1)/3*3 + 3) + 1
					last := time.Date(t.Year(), qEnd, 0, 0, 0, 0, 0, time.UTC)
					return types.Timestamp{Time: last}
				}),
			),
		),

		// Custom Member Function 2: timestamp.format_date(string) -> string
		cel.Function("format_date",
			cel.MemberOverload("timestamp_format_date_string",
				[]*cel.Type{cel.TimestampType, cel.StringType},
				cel.StringType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					ts, ok := lhs.(types.Timestamp)
					if !ok {
						return types.ValOrErr(lhs, "lhs must be a timestamp")
					}
					layout, ok := rhs.(types.String)
					if !ok {
						return types.ValOrErr(rhs, "rhs must be a string")
					}
					return types.String(ts.Time.Format(string(layout)))
				}),
			),
		),
	)

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize CEL environment: %w", err)
	}

	return &Evaluator{env: env}, nil
}

func (e *Evaluator) withFormatConstants(ctx map[string]any) map[string]any {
	merged := make(map[string]any, len(ctx)+len(dateFormatConstants))
	for k, v := range dateFormatConstants {
		merged[k] = v
	}
	for k, v := range ctx {
		merged[k] = v
	}
	return merged
}

// EvaluateExpr compiles and runs a single CEL string against the current context.
func (e *Evaluator) EvaluateExpr(exprStr string, ctx map[string]any) (any, error) {
	if len(exprStr) > maxExprLength {
		return nil, fmt.Errorf("expression length %d exceeds maximum of %d characters", len(exprStr), maxExprLength)
	}

	ast, iss := e.env.Compile(exprStr)
	if iss.Err() != nil {
		return nil, fmt.Errorf("compile error: %w", iss.Err())
	}

	prg, err := e.env.Program(ast, cel.CostLimit(maxEvalCost))
	if err != nil {
		return nil, fmt.Errorf("program creation error: %w", err)
	}

	out, _, err := prg.Eval(e.withFormatConstants(ctx))
	if err != nil {
		return nil, fmt.Errorf("eval error: %w", err)
	}

	return out.Value(), nil
}

// exprVisitor is called for each scheduler_cel: expression found during payload traversal.
// It receives the expression body (without the "scheduler_cel:" prefix) and returns the
// replacement value (or the zero value if only validating) and an error.
type exprVisitor func(expr string) (any, error)

// walkPayload recursively traverses map/list data structures, calling visitor
// for each scheduler_cel:-prefixed string. Used by both ProcessPayload and ValidatePayload.
func (e *Evaluator) walkPayload(data any, visitor exprVisitor, depth int, evalCount *int) (any, error) {
	if depth > maxPayloadDepth {
		return nil, fmt.Errorf("payload nesting depth exceeds maximum of %d", maxPayloadDepth)
	}

	if data == nil {
		return nil, nil
	}

	switch v := data.(type) {
	case string:
		if strings.HasPrefix(v, celPrefix) {
			*evalCount++
			if *evalCount > maxEvalCount {
				return nil, fmt.Errorf("number of CEL expressions exceeds maximum of %d", maxEvalCount)
			}
			return visitor(v[len(celPrefix):])
		}
		return v, nil

	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			evalVal, err := e.walkPayload(val, visitor, depth+1, evalCount)
			if err != nil {
				return nil, fmt.Errorf("field '%s': %w", key, err)
			}
			result[key] = evalVal
		}
		return result, nil

	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			evalVal, err := e.walkPayload(val, visitor, depth+1, evalCount)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			result[i] = evalVal
		}
		return result, nil

	default:
		return v, nil
	}
}

// ProcessPayload recursively traverses map/list data structures, replacing
// scheduler_cel:-prefixed expression strings with evaluated dynamic values.
func (e *Evaluator) ProcessPayload(data any, ctx map[string]any) (any, error) {
	evalCount := 0
	return e.walkPayload(data, func(expr string) (any, error) {
		return e.EvaluateExpr(expr, ctx)
	}, 0, &evalCount)
}

// ValidatePayload recursively walks a payload structure and compiles any
// scheduler_cel: expressions without evaluating them. This is intended for API-time
// validation when users create or update jobs, so that malformed expressions
// are rejected early. It enforces expression length, nesting depth, and eval
// count limits. The runtime cost limit (maxEvalCost) is only enforced during
// actual evaluation in ProcessPayload, not here.
func (e *Evaluator) ValidatePayload(data any) error {
	evalCount := 0
	_, err := e.walkPayload(data, func(expr string) (any, error) {
		if len(expr) > maxExprLength {
			return nil, fmt.Errorf("expression length %d exceeds maximum of %d characters", len(expr), maxExprLength)
		}
		_, iss := e.env.Compile(expr)
		if iss.Err() != nil {
			return nil, fmt.Errorf("compile error: %w", iss.Err())
		}
		return nil, nil
	}, 0, &evalCount)
	return err
}
