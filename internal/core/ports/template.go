package ports

// PayloadValidator checks that all templated expressions in a payload are
// syntactically valid. Used at API time to reject malformed expressions early.
type PayloadValidator interface {
	ValidatePayload(data any) error
}

// PayloadResolver evaluates templated expressions in a payload, replacing
// them with computed values. Used at job execution time.
type PayloadResolver interface {
	ProcessPayload(data any, ctx map[string]any) (any, error)
}
