package engine

import (
	"github.com/utkrusht/market-platform/backend/internal/market"
)

// rollingState keeps per-instrument in-memory indicator state. The
// engine is a single consumer per instrument (partitioned by the stream
// consumer), so no locking is needed inside one instrument's state.
type rollingState struct {
	closes    []float64 // ring of recent LTPs sampled once per minute
	volumes   []int64   // per-minute traded volume samples
	lastMinute int64    // unix minute of the last sample

	prevDayHigh float64 // loaded from candles/instrument metadata at boot
	prevDayLow  float64
}

const (
	smaWindow = 20
	rsiWindow = 14
	volWindow = 20
)

// update ingests a tick and returns the metric snapshot the rule DSL
// evaluates against.
func (s *rollingState) update(q *market.Quote) map[string]float64 {
	minute := q.EventTime.Unix() / 60
	if minute != s.lastMinute {
		// New minute: sample close & volume for indicator windows.
		s.closes = appendCap(s.closes, q.LTP, smaWindow+rsiWindow+1)
		s.volumes = appendCapInt(s.volumes, q.Volume, volWindow+1)
		s.lastMinute = minute
	} else if n := len(s.closes); n > 0 {
		s.closes[n-1] = q.LTP // refine current minute's close
	}

	m := map[string]float64{
		"ltp":                  q.LTP,
		"price_change_percent": q.ChangePercent(),
		"current_volume":       float64(q.Volume),
	}
	if q.WeekAgoClose > 0 {
		m["week_change_percent"] = q.WeekChangePercent()
	}
	if s.prevDayHigh > 0 {
		m["previous_day_high"] = s.prevDayHigh
	}
	if s.prevDayLow > 0 {
		m["previous_day_low"] = s.prevDayLow
	}
	if q.PrevClose > 0 && q.Open > 0 {
		m["gap_percent"] = (q.Open - q.PrevClose) / q.PrevClose * 100
	}
	if av := avgVolumeDelta(s.volumes); av > 0 {
		m["avg_volume"] = av
	}
	if sma, ok := sma(s.closes, smaWindow); ok {
		m["sma_20"] = sma
	}
	if rsi, ok := rsi(s.closes, rsiWindow); ok {
		m["rsi_14"] = rsi
	}
	return m
}

func appendCap(s []float64, v float64, max int) []float64 {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

func appendCapInt(s []int64, v int64, max int) []int64 {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// avgVolumeDelta: Kite reports cumulative day volume; per-minute traded
// volume is the delta between samples. Average over the window.
func avgVolumeDelta(cum []int64) float64 {
	if len(cum) < 2 {
		return 0
	}
	var sum, n float64
	for i := 1; i < len(cum); i++ {
		d := cum[i] - cum[i-1]
		if d >= 0 {
			sum += float64(d)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

func sma(closes []float64, window int) (float64, bool) {
	if len(closes) < window {
		return 0, false
	}
	var sum float64
	for _, c := range closes[len(closes)-window:] {
		sum += c
	}
	return sum / float64(window), true
}

// rsi computes Wilder's RSI over the sampled closes.
func rsi(closes []float64, window int) (float64, bool) {
	if len(closes) < window+1 {
		return 0, false
	}
	closes = closes[len(closes)-window-1:]
	var gain, loss float64
	for i := 1; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		if d > 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	if loss == 0 {
		return 100, true
	}
	rs := (gain / float64(window)) / (loss / float64(window))
	return 100 - 100/(1+rs), true
}
