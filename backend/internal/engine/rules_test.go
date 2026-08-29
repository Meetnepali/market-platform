package engine

import (
	"encoding/json"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestParseRule_Valid(t *testing.T) {
	raw := json.RawMessage(`{
		"name": "Breakout + Volume Spike",
		"all": [
			{"metric": "price_change_percent", "op": ">=", "value": 3},
			{"metric": "current_volume", "op": ">=", "ref": "avg_volume", "mult": 2},
			{"metric": "ltp", "op": ">", "ref": "previous_day_high"}
		],
		"signal_type": "BREAKOUT_VOLUME_SPIKE",
		"cooldown_seconds": 300
	}`)
	r, err := ParseRule(raw)
	if err != nil {
		t.Fatalf("expected valid rule, got %v", err)
	}
	if r.SignalType != "BREAKOUT_VOLUME_SPIKE" {
		t.Errorf("signal type = %q", r.SignalType)
	}
}

func TestParseRule_Rejects(t *testing.T) {
	cases := map[string]string{
		"unknown metric":   `{"all":[{"metric":"__import__","op":">","value":1}],"signal_type":"X"}`,
		"unknown op":       `{"all":[{"metric":"ltp","op":"=~","value":1}],"signal_type":"X"}`,
		"unknown ref":      `{"all":[{"metric":"ltp","op":">","ref":"os_system"}],"signal_type":"X"}`,
		"no conditions":    `{"signal_type":"X"}`,
		"no signal type":   `{"all":[{"metric":"ltp","op":">","value":1}]}`,
		"unknown field":    `{"all":[{"metric":"ltp","op":">","value":1}],"signal_type":"X","exec":"rm -rf"}`,
		"missing operand":  `{"all":[{"metric":"ltp","op":">"}],"signal_type":"X"}`,
	}
	for name, raw := range cases {
		if _, err := ParseRule(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: expected rejection, rule parsed", name)
		}
	}
}

func TestEval_BreakoutRule(t *testing.T) {
	r := &Rule{
		All: []Condition{
			{Metric: "price_change_percent", Op: ">=", Value: f(3)},
			{Metric: "current_volume", Op: ">=", Ref: "avg_volume", Mult: 2},
			{Metric: "ltp", Op: ">", Ref: "previous_day_high"},
		},
		SignalType: "BREAKOUT_VOLUME_SPIKE",
	}

	match := map[string]float64{
		"price_change_percent": 3.4,
		"current_volume":       9_000_000,
		"avg_volume":           4_000_000,
		"ltp":                  1490,
		"previous_day_high":    1484,
	}
	if !r.Eval(match) {
		t.Error("expected match")
	}

	// Volume only 1.5x average → no signal.
	match["current_volume"] = 6_000_000
	if r.Eval(match) {
		t.Error("expected no match on weak volume")
	}
}

func TestEval_MissingMetricIsFalseNotError(t *testing.T) {
	r := &Rule{
		All:        []Condition{{Metric: "rsi_14", Op: "<", Value: f(30)}},
		SignalType: "OVERSOLD",
	}
	// No RSI yet (not enough history) → condition simply false.
	if r.Eval(map[string]float64{"ltp": 100}) {
		t.Error("missing metric must evaluate false")
	}
}

func TestEval_AnyGroup(t *testing.T) {
	r := &Rule{
		Any: []Condition{
			{Metric: "gap_percent", Op: ">=", Value: f(2)},
			{Metric: "gap_percent", Op: "<=", Value: f(-2)},
		},
		SignalType: "GAP",
	}
	if !r.Eval(map[string]float64{"gap_percent": -2.5}) {
		t.Error("gap down should match")
	}
	if r.Eval(map[string]float64{"gap_percent": 0.4}) {
		t.Error("small gap should not match")
	}
}
