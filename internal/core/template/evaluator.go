package template

import (
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	maxExprLength   = 1024
	maxEvalCost     = 10_000
	maxPayloadDepth = 20
	maxEvalCount    = 50
)

// Evaluator manages the CEL environment and executes templated expressions.
type Evaluator struct {
	env *cel.Env
}

// NewEvaluator sets up the CEL environment with context variables and custom date functions.
func NewEvaluator() (*Evaluator, error) {
	env, err := cel.NewEnv(
		// Inject standard context variables
		cel.Variable("now", cel.TimestampType),
		cel.Variable("job_id", cel.StringType),

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
					newTime := ts.Time.AddDate(0, int(months), 0)
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

	if err != nil {
		return nil, fmt.Errorf("failed to initialize CEL environment: %w", err)
	}

	return &Evaluator{env: env}, nil
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

	out, _, err := prg.Eval(ctx)
	if err != nil {
		return nil, fmt.Errorf("eval error: %w", err)
	}

	return out.Value(), nil
}

// ProcessPayload recursively traverses map/list data structures, replacing
// expression strings with evaluated dynamic values.
func (e *Evaluator) ProcessPayload(data any, ctx map[string]any) (any, error) {
	evalCount := 0
	return e.processPayload(data, ctx, 0, &evalCount)
}

func (e *Evaluator) processPayload(data any, ctx map[string]any, depth int, evalCount *int) (any, error) {
	if depth > maxPayloadDepth {
		return nil, fmt.Errorf("payload nesting depth exceeds maximum of %d", maxPayloadDepth)
	}

	if data == nil {
		return nil, nil
	}

	switch v := data.(type) {
	case string:
		if len(v) > 4 && v[:4] == "cel:" {
			*evalCount++
			if *evalCount > maxEvalCount {
				return nil, fmt.Errorf("number of CEL expressions exceeds maximum of %d", maxEvalCount)
			}
			return e.EvaluateExpr(v[4:], ctx)
		}
		return v, nil

	case map[string]any:
		result := make(map[string]any, len(v))
		for key, val := range v {
			evalVal, err := e.processPayload(val, ctx, depth+1, evalCount)
			if err != nil {
				return nil, fmt.Errorf("field '%s': %w", key, err)
			}
			result[key] = evalVal
		}
		return result, nil

	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			evalVal, err := e.processPayload(val, ctx, depth+1, evalCount)
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

// ValidatePayload recursively walks a payload structure and compiles any
// "cel:" expressions without evaluating them. This is intended for API-time
// validation when users create or update jobs, so that malformed expressions
// are rejected early. It enforces expression length, nesting depth, and eval
// count limits. The runtime cost limit (maxEvalCost) is only enforced during
// actual evaluation in ProcessPayload, not here.
func (e *Evaluator) ValidatePayload(data any) error {
	evalCount := 0
	return e.validatePayload(data, 0, &evalCount)
}

func (e *Evaluator) validatePayload(data any, depth int, evalCount *int) error {
	if depth > maxPayloadDepth {
		return fmt.Errorf("payload nesting depth exceeds maximum of %d", maxPayloadDepth)
	}

	if data == nil {
		return nil
	}

	switch v := data.(type) {
	case string:
		if len(v) > 4 && v[:4] == "cel:" {
			*evalCount++
			if *evalCount > maxEvalCount {
				return fmt.Errorf("number of CEL expressions exceeds maximum of %d", maxEvalCount)
			}
			expr := v[4:]
			if len(expr) > maxExprLength {
				return fmt.Errorf("expression length %d exceeds maximum of %d characters", len(expr), maxExprLength)
			}
			_, iss := e.env.Compile(expr)
			if iss.Err() != nil {
				return fmt.Errorf("compile error: %w", iss.Err())
			}
		}
		return nil

	case map[string]any:
		for key, val := range v {
			if err := e.validatePayload(val, depth+1, evalCount); err != nil {
				return fmt.Errorf("field '%s': %w", key, err)
			}
		}
		return nil

	case []any:
		for i, val := range v {
			if err := e.validatePayload(val, depth+1, evalCount); err != nil {
				return fmt.Errorf("index %d: %w", i, err)
			}
		}
		return nil

	default:
		return nil
	}
}
