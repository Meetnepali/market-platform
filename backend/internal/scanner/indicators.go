package scanner

// Pure indicator math over daily series (oldest first). Kept free of
// I/O so every function is trivially unit-testable.

// sma returns the simple moving average of the last n values, or 0 if
// there is not enough data.
func sma(vals []float64, n int) float64 {
	if n <= 0 || len(vals) < n {
		return 0
	}
	sum := 0.0
	for _, v := range vals[len(vals)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// smaAgo returns the n-period SMA as it stood `ago` bars back.
func smaAgo(vals []float64, n, ago int) float64 {
	if ago <= 0 {
		return sma(vals, n)
	}
	if len(vals) < n+ago {
		return 0
	}
	return sma(vals[:len(vals)-ago], n)
}

// rsi computes Wilder's RSI over the given period for the latest bar.
// Returns 50 (neutral) when there is insufficient data.
func rsi(closes []float64, period int) float64 {
	if period <= 0 || len(closes) < period+1 {
		return 50
	}
	// Seed with the first `period` changes, then Wilder-smooth the rest.
	avgGain, avgLoss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		d := closes[i] - closes[i-1]
		if d > 0 {
			avgGain += d
		} else {
			avgLoss -= d
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := period + 1; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if d > 0 {
			gain = d
		} else {
			loss = -d
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// consecDown counts consecutive down closes ending at the latest bar.
func consecDown(closes []float64) int {
	n := 0
	for i := len(closes) - 1; i > 0; i-- {
		if closes[i] < closes[i-1] {
			n++
		} else {
			break
		}
	}
	return n
}

// pctBelowHigh returns how far (in %) the latest close sits below the
// highest close of the last n bars. 0 means at the high.
func pctBelowHigh(closes []float64, n int) float64 {
	if len(closes) == 0 {
		return 0
	}
	window := closes
	if len(window) > n {
		window = window[len(window)-n:]
	}
	hi := window[0]
	for _, c := range window {
		if c > hi {
			hi = c
		}
	}
	if hi <= 0 {
		return 0
	}
	return (hi - closes[len(closes)-1]) / hi * 100
}

// pctAboveLow returns how far (in %) the latest close sits above the
// lowest close of the last n bars. 0 means at the low.
func pctAboveLow(closes []float64, n int) float64 {
	if len(closes) == 0 {
		return 0
	}
	window := closes
	if len(window) > n {
		window = window[len(window)-n:]
	}
	lo := window[0]
	for _, c := range window {
		if c < lo {
			lo = c
		}
	}
	if lo <= 0 {
		return 0
	}
	return (closes[len(closes)-1] - lo) / lo * 100
}

// totalReturn returns the % change over the last n bars.
func totalReturn(closes []float64, n int) float64 {
	if len(closes) < n+1 {
		return 0
	}
	base := closes[len(closes)-1-n]
	if base <= 0 {
		return 0
	}
	return (closes[len(closes)-1] - base) / base * 100
}

// avgVolume returns the mean volume of the last n bars.
func avgVolume(vols []int64, n int) float64 {
	if n <= 0 || len(vols) < n {
		return 0
	}
	sum := 0.0
	for _, v := range vols[len(vols)-n:] {
		sum += float64(v)
	}
	return sum / float64(n)
}

// downDayVolumeRatio measures selling pressure: average volume on the
// down days within the last `recent` bars, divided by the `base`-bar
// average volume. Below 1.0 means the decline is happening on
// below-normal volume — sellers exhausting rather than initiating.
// Returns 1.0 (neutral) when there are no recent down days.
func downDayVolumeRatio(closes []float64, vols []int64, recent, base int) float64 {
	baseAvg := avgVolume(vols, base)
	if baseAvg <= 0 || len(closes) < 2 || len(closes) != len(vols) {
		return 1
	}
	start := len(closes) - recent
	if start < 1 {
		start = 1
	}
	sum, n := 0.0, 0
	for i := start; i < len(closes); i++ {
		if closes[i] < closes[i-1] {
			sum += float64(vols[i])
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return (sum / float64(n)) / baseAvg
}

// maxAbsDailyMove returns the largest single-day % move (either
// direction) within the last n bars. Used to skip stocks in circuit /
// news-shock territory where mean reversion is unreliable.
func maxAbsDailyMove(closes []float64, n int) float64 {
	if len(closes) < 2 {
		return 0
	}
	start := len(closes) - n
	if start < 1 {
		start = 1
	}
	maxMove := 0.0
	for i := start; i < len(closes); i++ {
		if closes[i-1] <= 0 {
			continue
		}
		m := closes[i]/closes[i-1] - 1
		if m < 0 {
			m = -m
		}
		if m*100 > maxMove {
			maxMove = m * 100
		}
	}
	return maxMove
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
