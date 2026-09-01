package scanner

import (
	"math"
	"testing"
	"time"
)

// mkHistory builds a History from close prices with a constant volume,
// spacing bars one day apart.
func mkHistory(symbol string, closes []float64, vols []int64) *History {
	h := &History{Symbol: symbol}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range closes {
		v := int64(1_000_000)
		if vols != nil {
			v = vols[i]
		}
		h.Bars = append(h.Bars, Bar{
			Time: t0.AddDate(0, 0, i), Open: c, High: c * 1.01, Low: c * 0.99, Close: c, Volume: v,
		})
	}
	return h
}

// uptrendWithDip: 250 bars rising ~0.3%/day, then a 4-day ~5% dip on
// shrinking volume — the exact setup the scanner exists to find.
func uptrendWithDip() *History {
	closes := make([]float64, 0, 250)
	vols := make([]int64, 0, 250)
	price := 500.0
	for i := 0; i < 246; i++ {
		price *= 1.003
		closes = append(closes, price)
		vols = append(vols, 1_000_000)
	}
	for i := 0; i < 4; i++ {
		price *= 0.9870
		closes = append(closes, price)
		vols = append(vols, int64(700_000-100_000*i)) // fading volume
	}
	return mkHistory("DIPSTOCK", closes, vols)
}

// flatIndex gives the stock easy positive relative strength.
func flatIndex() *History {
	closes := make([]float64, 250)
	for i := range closes {
		closes[i] = 20000
	}
	return mkHistory(NiftyIndexSymbol, closes, nil)
}

func TestEvaluatePicksUptrendDip(t *testing.T) {
	r, ok := Evaluate(uptrendWithDip(), flatIndex(), DefaultConfig())
	if !ok {
		t.Fatal("expected uptrend-with-dip stock to qualify")
	}
	if r.Score <= 0 || r.Score > 100 {
		t.Fatalf("score out of range: %f", r.Score)
	}
	if r.Metrics["down_days"] < 3 {
		t.Fatalf("expected >=3 down days, got %f", r.Metrics["down_days"])
	}
	if r.Metrics["rsi2"] > 15 {
		t.Fatalf("expected deeply oversold RSI2, got %f", r.Metrics["rsi2"])
	}
	if len(r.Reasons) == 0 {
		t.Fatal("expected human-readable reasons")
	}
}

func TestEvaluateRejectsDowntrend(t *testing.T) {
	// Steady decline: below its 200-day average — gate must reject it
	// no matter how oversold it looks.
	closes := make([]float64, 250)
	price := 1000.0
	for i := range closes {
		price *= 0.997
		closes[i] = price
	}
	if _, ok := Evaluate(mkHistory("FALLING", closes, nil), flatIndex(), DefaultConfig()); ok {
		t.Fatal("downtrending stock must not qualify")
	}
}

func TestEvaluateRejectsNoDip(t *testing.T) {
	// Strong uptrend, no pullback: nothing to buy into.
	closes := make([]float64, 250)
	price := 500.0
	for i := range closes {
		price *= 1.003
		closes[i] = price
	}
	if _, ok := Evaluate(mkHistory("RUNNER", closes, nil), flatIndex(), DefaultConfig()); ok {
		t.Fatal("stock at its highs must not qualify")
	}
}

func TestEvaluateRejectsCrash(t *testing.T) {
	// Uptrend then a single -18% day: news shock, not a dip.
	closes := make([]float64, 250)
	price := 500.0
	for i := range closes {
		price *= 1.003
		closes[i] = price
	}
	closes[len(closes)-1] = closes[len(closes)-2] * 0.82
	if _, ok := Evaluate(mkHistory("CRASHED", closes, nil), flatIndex(), DefaultConfig()); ok {
		t.Fatal("crashed stock must not qualify")
	}
}

func TestEvaluateRejectsIlliquid(t *testing.T) {
	h := uptrendWithDip()
	for i := range h.Bars {
		h.Bars[i].Volume = 100 // trades peanuts
	}
	if _, ok := Evaluate(h, flatIndex(), DefaultConfig()); ok {
		t.Fatal("illiquid stock must not qualify")
	}
}

func TestRSIKnownValues(t *testing.T) {
	// All-up series → RSI 100; all-down → RSI ~0.
	up := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	if got := rsi(up, 2); got != 100 {
		t.Fatalf("all-gains RSI = %f, want 100", got)
	}
	down := []float64{8, 7, 6, 5, 4, 3, 2, 1}
	if got := rsi(down, 2); got > 1e-9 {
		t.Fatalf("all-losses RSI = %f, want ~0", got)
	}
	// Insufficient data → neutral.
	if got := rsi([]float64{1, 2}, 14); got != 50 {
		t.Fatalf("short series RSI = %f, want 50", got)
	}
}

func TestIndicatorBasics(t *testing.T) {
	closes := []float64{10, 11, 12, 11.5, 11, 10.5}
	if got := consecDown(closes); got != 3 {
		t.Fatalf("consecDown = %d, want 3", got)
	}
	if got := sma([]float64{2, 4, 6}, 3); got != 4 {
		t.Fatalf("sma = %f, want 4", got)
	}
	if got := pctBelowHigh(closes, 6); math.Abs(got-12.5) > 1e-9 {
		t.Fatalf("pctBelowHigh = %f, want 12.5", got)
	}
	vols := []int64{100, 100, 100, 50, 50, 50}
	// Down days are the last three bars (vol 50 each); base avg over 6 = 75.
	if got := downDayVolumeRatio(closes, vols, 5, 6); math.Abs(got-50.0/75.0) > 1e-9 {
		t.Fatalf("downDayVolumeRatio = %f, want %f", got, 50.0/75.0)
	}
}
