import { useEffect, useState } from 'react'
import { api, type Quote, type StockDetails } from '../lib/api'
import { PriceChart } from './PriceChart'

/**
 * Groww-style stock detail drawer: Performance (day + 52-week range
 * sliders, open/close/volume), Fundamentals grid, and computed risk
 * metrics (beta / alpha / volatility) with plain-language hints.
 */
export function StockDetail({ quote, onClose }: { quote: Quote; onClose: () => void }) {
  const [d, setD] = useState<StockDetails | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    setD(null)
    setError('')
    api
      .stockDetails(quote.instrument, quote.exchange)
      .then(setD)
      .catch(() => setError('Could not load details for this stock'))
  }, [quote.instrument, quote.exchange])

  // close on Escape
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const chg = quote.previous_close
    ? ((quote.ltp - quote.previous_close) / quote.previous_close) * 100
    : 0
  const isUp = chg >= 0

  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside className="drawer" onClick={(e) => e.stopPropagation()}>
        <div className="drawer-head">
          <div>
            <h2>
              <span className="stock-symbol">{quote.instrument}</span>
              <span className="exch-badge">{quote.exchange}</span>
            </h2>
            {d?.name && <p className="company-name">{d.name}</p>}
          </div>
          <button className="drawer-close" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        <div className="drawer-price-card">
          <span className="main-price">
            ₹{quote.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}
          </span>
          <span className={`chg-pill ${isUp ? 'up' : 'down'}`}>
            <span className="arrow">{isUp ? '▲' : '▼'}</span>
            {Math.abs(chg).toFixed(2)}% today
          </span>
        </div>

        <PriceChart symbol={quote.instrument} exchange={quote.exchange} />

        {/* ── Performance ── */}
        <div>
          <h3>Performance</h3>
          <RangeSlider label="Today's Range" low={quote.low} high={quote.high} value={quote.ltp} />
          {d?.fifty_two_week_low ? (
            <RangeSlider
              label="52-Week Range"
              low={d.fifty_two_week_low}
              high={d.fifty_two_week_high ?? quote.ltp}
              value={quote.ltp}
            />
          ) : null}
          <div className="kv-row">
            <KV k="Open" v={`₹${fmtNum(quote.open)}`} />
            <KV k="Prev Close" v={`₹${fmtNum(quote.previous_close)}`} />
            <KV k="Volume" v={fmtVol(quote.volume)} />
          </div>
        </div>

        {/* ── Fundamentals ── */}
        <div>
          <h3>Fundamentals</h3>
          {error && <p className="hint-text">{error}</p>}
          {!d && !error && <p className="hint-text">Loading company fundamentals…</p>}
          {d && (
            <div className="fund-grid">
              <KV k="Market Cap" v={fmtCr(d.market_cap)} hint="Total valuation" />
              <KV k="P/E (TTM)" v={fmt2(d.trailing_pe)} hint="Price to Earnings" />
              <KV k="P/B Ratio" v={fmt2(d.price_to_book)} hint="Price to Book Net Worth" />
              <KV k="EPS (TTM)" v={d.trailing_eps ? `₹${fmt2(d.trailing_eps)}` : '—'} hint="Earnings Per Share" />
              <KV
                k="Div. Yield"
                v={d.dividend_yield ? `${(d.dividend_yield * 100).toFixed(2)}%` : '—'}
                hint="Annual Dividend Yield"
              />
              <KV k="Book Value" v={d.book_value ? `₹${fmt2(d.book_value)}` : '—'} hint="Net Asset Value/Share" />
              <KV
                k="Debt / Equity"
                v={d.debt_to_equity ? (d.debt_to_equity / 100).toFixed(2) : '—'}
                hint="Leverage ratio"
              />
              <KV
                k="ROE"
                v={d.roe ? `${(d.roe * 100).toFixed(1)}%` : '—'}
                hint="Return on Equity"
              />
            </div>
          )}
        </div>

        {/* ── Risk (computed) ── */}
        {d && (d.beta || d.vol_annual) ? (
          <div>
            <h3>Risk vs NIFTY 50 (1Y Daily)</h3>
            <div className="fund-grid">
              <KV
                k="Beta (β)"
                v={fmt2(d.beta)}
                hint={betaHint(d.beta ?? 0)}
              />
              <KV
                k="Alpha (α)"
                v={d.alpha_annual != null ? `${d.alpha_annual >= 0 ? '+' : ''}${d.alpha_annual}%/yr` : '—'}
                hint={d.alpha_annual != null && d.alpha_annual >= 0 ? 'Outperformed risk-adjusted index' : 'Underperformed risk benchmark'}
              />
              <KV k="Volatility" v={d.vol_annual ? `${d.vol_annual}%/yr` : '—'} hint="Annualized standard deviation" />
              <KV
                k="1Y Return"
                v={d.year_return != null ? `${d.year_return >= 0 ? '+' : ''}${d.year_return}%` : '—'}
                hint="12-month trailing return"
              />
            </div>
          </div>
        ) : null}
      </aside>
    </div>
  )
}

function betaHint(b: number): string {
  if (!b) return '—'
  if (b > 1.3) return 'Higher swings than NIFTY'
  if (b > 0.8) return 'Moves closely with NIFTY'
  return 'Lower swings than market'
}

function RangeSlider({ label, low, high, value }: { label: string; low: number; high: number; value: number }) {
  const pct = high > low ? Math.min(100, Math.max(0, ((value - low) / (high - low)) * 100)) : 50
  return (
    <div className="range-card">
      <div className="range-labels">
        <span>
          {label} Low<br />
          <b>₹{fmtNum(low)}</b>
        </span>
        <span style={{ textAlign: 'right' }}>
          {label} High<br />
          <b>₹{fmtNum(high)}</b>
        </span>
      </div>
      <div className="range-track">
        <div className="range-marker" style={{ left: `${pct}%` }} />
      </div>
    </div>
  )
}

function KV({ k, v, hint }: { k: string; v: string; hint?: string }) {
  return (
    <div className="kv-card" title={hint}>
      <span className="kv-key">{k}</span>
      <span className="kv-val">{v}</span>
      {hint && <span className="kv-hint">{hint}</span>}
    </div>
  )
}

function fmtNum(n?: number): string {
  return n != null && n > 0
    ? n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    : '—'
}

function fmt2(n?: number): string {
  return n != null && n !== 0 ? n.toFixed(2) : '—'
}

function fmtCr(n?: number): string {
  if (!n) return '—'
  return `₹${Math.round(n / 1e7).toLocaleString('en-IN')} Cr`
}

function fmtVol(vol?: number): string {
  if (!vol) return '—'
  if (vol >= 10000000) return `${(vol / 10000000).toFixed(2)} Cr`
  if (vol >= 100000) return `${(vol / 100000).toFixed(2)} L`
  return vol.toLocaleString('en-IN')
}
