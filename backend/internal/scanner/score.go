package scanner

import "fmt"

// Config holds every tunable threshold of the pullback-in-uptrend
// score. Defaults encode the strategy discussed in the docs: buy dips
// in strong stocks once selling pressure fades. All percentages.
type Config struct {
	MinBars int // minimum daily history required to score at all

	// Gates — a stock failing any gate is skipped entirely.
	TrendSMA        int     // long-term trend average (200)
	TrendSlopeBars  int     // SMA must be higher than it was this many bars ago
	RelStrengthBars int     // relative-strength lookback vs NIFTY (126 ≈ 6 months)
	MinTurnover     float64 // min 20-day avg daily turnover in ₹ (liquidity)
	MaxDailyMove    float64 // skip if any day in the last 10 moved more than this %

	// Trigger — at least one must fire, plus a real pullback.
	OversoldRSI2 float64 // RSI(2) below this is oversold
	MinDownDays  int     // or this many consecutive down closes
	NearLowPct   float64 // or within this % of the 10-day low
	MinPullback  float64 // pullback from 20-day high must be at least this…
	MaxPullback  float64 // …and no more than this (deeper = news risk, not a dip)
}

func DefaultConfig() Config {
	return Config{
		MinBars:         210,
		TrendSMA:        200,
		TrendSlopeBars:  20,
		RelStrengthBars: 126,
		MinTurnover:     5e7, // ₹5 crore/day
		MaxDailyMove:    15,
		OversoldRSI2:    10,
		MinDownDays:     3,
		NearLowPct:      2,
		MinPullback:     3,
		MaxPullback:     15,
	}
}

// Result is one buy candidate with its composite score and the
// human-readable reasons the UI shows next to it.
type Result struct {
	Symbol       string
	InstrumentID int64
	Close        float64
	Score        float64
	Rank         int
	Reasons      []string
	Metrics      map[string]float64
}

// Evaluate scores one stock against the index benchmark. Returns
// (nil, false) when any gate fails or no trigger fires — the stock
// simply doesn't belong in the buy tab today.
func Evaluate(h *History, index *History, cfg Config) (*Result, bool) {
	closes := h.Closes()
	vols := h.Volumes()
	if len(closes) < cfg.MinBars {
		return nil, false
	}
	last := closes[len(closes)-1]

	// ── Gates: only strong, liquid, orderly stocks qualify ──────────
	ma := sma(closes, cfg.TrendSMA)
	maPrev := smaAgo(closes, cfg.TrendSMA, cfg.TrendSlopeBars)
	if ma <= 0 || last <= ma || ma <= maPrev {
		return nil, false // not in a rising long-term uptrend
	}

	relStrength := totalReturn(closes, cfg.RelStrengthBars) -
		totalReturn(index.Closes(), cfg.RelStrengthBars)
	if relStrength < 0 {
		return nil, false // lagging the index — dip in a laggard, skip
	}

	if avgVolume(vols, 20)*last < cfg.MinTurnover {
		return nil, false // too illiquid to trust the bounce
	}
	if maxAbsDailyMove(closes, 10) > cfg.MaxDailyMove {
		return nil, false // circuit / news shock territory
	}

	// ── Trigger: has it actually pulled back? ───────────────────────
	pullback := pctBelowHigh(closes, 20)
	if pullback < cfg.MinPullback || pullback > cfg.MaxPullback {
		return nil, false
	}

	rsi2 := rsi(closes, 2)
	downDays := consecDown(closes)
	aboveLow := pctAboveLow(closes, 10)

	oversold := rsi2 < cfg.OversoldRSI2
	downStreak := downDays >= cfg.MinDownDays
	nearLow := aboveLow <= cfg.NearLowPct
	if !oversold && !downStreak && !nearLow {
		return nil, false
	}

	// ── Quality + composite score (0–100) ───────────────────────────
	// Down-day volume vs normal: < 1.0 means sellers are drying up.
	volRatio := downDayVolumeRatio(closes, vols, 5, 50)

	depthScore := clamp01((cfg.OversoldRSI2 - rsi2) / cfg.OversoldRSI2) // deeper oversold = better
	streakScore := clamp01(float64(downDays) / 5)
	volScore := clamp01((1.2 - volRatio) / 0.7) // ratio 1.2→0, 0.5→1
	trendScore := clamp01(relStrength / 20)     // 20pp ahead of NIFTY = max
	// Pullback sweet spot: 3–8% scores fully, fades toward MaxPullback.
	pullScore := 1.0
	if pullback > 8 {
		pullScore = clamp01((cfg.MaxPullback - pullback) / (cfg.MaxPullback - 8))
	}

	score := depthScore*30 + streakScore*15 + volScore*25 + trendScore*20 + pullScore*10

	var reasons []string
	reasons = append(reasons,
		fmt.Sprintf("Uptrend intact: above rising %d-day average", cfg.TrendSMA),
		fmt.Sprintf("Pulled back %.1f%% from its 20-day high", pullback))
	if oversold {
		reasons = append(reasons, fmt.Sprintf("Deeply oversold: RSI(2) at %.0f", rsi2))
	}
	if downStreak {
		reasons = append(reasons, fmt.Sprintf("%d straight down days", downDays))
	}
	if nearLow {
		reasons = append(reasons, "Sitting at its 10-day low")
	}
	if volRatio < 0.9 {
		reasons = append(reasons, fmt.Sprintf("Sellers fading: down-day volume %.0f%% of normal", volRatio*100))
	}
	if relStrength > 0 {
		reasons = append(reasons, fmt.Sprintf("Beating NIFTY by %.1fpp over 6 months", relStrength))
	}

	return &Result{
		Symbol:  h.Symbol,
		Close:   last,
		Score:   score,
		Reasons: reasons,
		Metrics: map[string]float64{
			"rsi2":            rsi2,
			"down_days":       float64(downDays),
			"pullback_pct":    pullback,
			"vol_ratio":       volRatio,
			"rel_strength_6m": relStrength,
			"sma200":          ma,
			"week_change_pct": totalReturn(closes, 5),
		},
	}, true
}
