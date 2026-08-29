// Package engine evaluates user strategies against the live tick stream.
//
// Strategies are a constrained, declarative JSON DSL — never user code.
// The schema is validated at save time (api) and defensively again at
// load time here, so an unknown metric or operator can never execute.
package engine

import (
	"encoding/json"
	"fmt"
)

// Supported metrics. Extending the DSL = adding a name here plus its
// computation in metrics.go. Nothing else.
var validMetrics = map[string]bool{
	"ltp":                  true,
	"price_change_percent": true,
	"week_change_percent":  true,
	"current_volume":       true,
	"avg_volume":           true,
	"previous_day_high":    true,
	"previous_day_low":     true,
	"gap_percent":          true,
	"sma_20":               true,
	"rsi_14":               true,
}

var validOps = map[string]bool{">": true, ">=": true, "<": true, "<=": true, "==": true}

// Condition compares a metric against either a literal value or another
// metric (ref), optionally scaled: metric op (value | mult × ref).
//
//	{"metric":"current_volume","op":">=","ref":"avg_volume","mult":2}
type Condition struct {
	Metric string   `json:"metric"`
	Op     string   `json:"op"`
	Value  *float64 `json:"value,omitempty"`
	Ref    string   `json:"ref,omitempty"`
	Mult   float64  `json:"mult,omitempty"` // default 1 when Ref is set
}

// Rule is the root of a strategy configuration. All/Any nest one level
// deep via groups if needed later; v1 keeps a flat conjunction plus an
// optional disjunction, which covers every first-generation strategy.
type Rule struct {
	Name            string      `json:"name"`
	All             []Condition `json:"all,omitempty"`
	Any             []Condition `json:"any,omitempty"`
	SignalType      string      `json:"signal_type"`
	CooldownSeconds int         `json:"cooldown_seconds"`
}

func ParseRule(raw json.RawMessage) (*Rule, error) {
	var r Rule
	dec := json.NewDecoder(bytesReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("invalid rule json: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (r *Rule) Validate() error {
	if r.SignalType == "" {
		return fmt.Errorf("signal_type is required")
	}
	if len(r.All) == 0 && len(r.Any) == 0 {
		return fmt.Errorf("rule must have at least one condition")
	}
	if r.CooldownSeconds < 0 {
		return fmt.Errorf("cooldown_seconds must be >= 0")
	}
	if r.CooldownSeconds == 0 {
		r.CooldownSeconds = 300 // sane default: don't re-fire for 5 min
	}
	for i, c := range append(append([]Condition{}, r.All...), r.Any...) {
		if err := c.validate(); err != nil {
			return fmt.Errorf("condition %d: %w", i, err)
		}
	}
	return nil
}

func (c *Condition) validate() error {
	if !validMetrics[c.Metric] {
		return fmt.Errorf("unknown metric %q", c.Metric)
	}
	if !validOps[c.Op] {
		return fmt.Errorf("unknown operator %q", c.Op)
	}
	if c.Value == nil && c.Ref == "" {
		return fmt.Errorf("condition needs value or ref")
	}
	if c.Ref != "" && !validMetrics[c.Ref] {
		return fmt.Errorf("unknown ref metric %q", c.Ref)
	}
	return nil
}

// Eval evaluates the rule against a metric snapshot. Missing metrics
// (e.g. RSI before enough history) make the condition false, never an
// error — a strategy silently waits until its inputs exist.
func (r *Rule) Eval(m map[string]float64) bool {
	for _, c := range r.All {
		if !c.eval(m) {
			return false
		}
	}
	if len(r.Any) > 0 {
		anyOK := false
		for _, c := range r.Any {
			if c.eval(m) {
				anyOK = true
				break
			}
		}
		if !anyOK {
			return false
		}
	}
	return true
}

func (c *Condition) eval(m map[string]float64) bool {
	left, ok := m[c.Metric]
	if !ok {
		return false
	}
	var right float64
	switch {
	case c.Ref != "":
		refVal, ok := m[c.Ref]
		if !ok {
			return false
		}
		mult := c.Mult
		if mult == 0 {
			mult = 1
		}
		right = refVal * mult
	case c.Value != nil:
		right = *c.Value
	default:
		return false
	}

	switch c.Op {
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	case "==":
		return left == right
	}
	return false
}
