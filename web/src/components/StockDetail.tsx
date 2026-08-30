import { useEffect, useState } from 'react'
import { api, type Quote, type StockDetails } from '../lib/api'

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

  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside className="drawer" onClick={(e) => e.stopPropagation()}>
        <header className="drawer-head">
          <div>
            <h2>
              {quote.instrument} <span className="exch">{quote.exchange}</span>
            </h2>
            {d?.name && <p className="company-name">{d.name}</p>}
          </div>
          <button onClick={onClose} aria-label="Close">✕</button>
        </header>

        <div className="drawer-price">
          <b>₹{quote.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}</b>
          <span className={chg >= 0 ? 'up' : 'down'}>
            {chg >= 0 ? '+' : ''}
            {chg.toFixed(2)}% today
          </span>
        </div>

        {/* ── Performance ── */}
        <h3>Performance</h3>
        <RangeSlider label="Today" low={quote.low} high={quote.high} value={quote.ltp} />
        {d?.fifty_two_week_low ? (
          <RangeSlider
            label="52 week"
            low={d.fifty_two_week_low}
            high={d.fifty_two_week_high ?? quote.ltp}
            value={quote.ltp}
          />
        ) : null}
        <div className="kv-row">
          <KV k="Open" v={fmtNum(quote.open)} />
          <KV k="Prev close" v={fmtNum(quote.previous_close)} />
          <KV k="Volume" v={quote.volume.toLocaleString('en-IN')} />
        </div>

        {/* ── Fundamentals ── */}
        <h3>Fundamentals</h3>
        {error && <p className="empty">{error}</p>}
        {!d && !error && <p className="empty">Loading fundamentals…</p>}
        {d && (
          <div className="fund-grid">
            <KV k="Market Cap" v={fmtCr(d.market_cap)} hint="Total value of the company" />
            <KV k="P/E (TTM)" v={fmt2(d.trailing_pe)} hint="Years of profit you pay for the price" />
            <KV k="P/B Ratio" v={fmt2(d.price_to_book)} hint="Price vs accounting net worth" />
            <KV k="EPS (TTM)" v={fmt2(d.trailing_eps)} hint="Profit per share, ₹/yr" />
            <KV k="Div. Yield" v={d.dividend_yield ? `${(d.dividend_yield * 100).toFixed(2)}%` : '—'} hint="Cash paid to you per year" />
            <KV k="Book Value" v={fmt2(d.book_value)} hint="Net worth per share" />
            <KV k="Debt / Equity" v={d.debt_to_equity ? (d.debt_to_equity / 100).toFixed(2) : '—'} hint="Borrowed vs own money" />
            <KV k="ROE" v={d.roe ? `${(d.roe * 100).toFixed(1)}%` : '—'} hint="How well profits use shareholder money" />
          </div>
        )}

        {/* ── Risk (computed) ── */}
        {d && (d.beta || d.vol_annual) ? (
          <>
            <h3>Risk vs NIFTY 50 <span className="count">computed, 1y daily</span></h3>
            <div className="fund-grid">
              <KV
                k="Beta (β)"
                v={fmt2(d.beta)}
                hint={betaHint(d.beta ?? 0)}
              />
              <KV
                k="Alpha (α)"
                v={d.alpha_annual != null ? `${d.alpha_annual >= 0 ? '+' : ''}${d.alpha_annual}%/yr` : '—'}
                hint={d.alpha_annual != null && d.alpha_annual >= 0 ? 'Beat its risk-adjusted expectation' : 'Underperformed its risk level'}
              />
              <KV k="Volatility" v={d.vol_annual ? `${d.vol_annual}%/yr` : '—'} hint="How bumpy the ride is" />
              <KV
                k="1Y Return"
                v={d.year_return != null ? `${d.year_return >= 0 ? '+' : ''}${d.year_return}%` : '—'}
                hint="Actual price change, 1 year"
              />
            </div>
          </>
        ) : null}
      </aside>
    </div>
  )
}

function betaHint(b: number): string {
  if (!b) return '—'
  if (b > 1.3) return 'Swings much harder than the market'
  if (b > 0.8) return 'Moves roughly with the market'
  return 'Calmer than the market'
}

function RangeSlider({ label, low, high, value }: { label: string; low: number; high: number; value: number }) {
  const pct = high > low ? Math.min(100, Math.max(0, ((value - low) / (high - low)) * 100)) : 50
  return (
    <div className="range-slider">
      <div className="range-labels">
        <span>{label} low<br /><b>{fmtNum(low)}</b></span>
        <span className="range-right">{label} high<br /><b>{fmtNum(high)}</b></span>
      </div>
      <div className="range-track">
        <div className="range-marker" style={{ left: `${pct}%` }}>▲</div>
      </div>
    </div>
  )
}

function KV({ k, v, hint }: { k: string; v: string; hint?: string }) {
  return (
    <div className="kv" title={hint}>
      <span className="kv-key">{k}</span>
      <span className="kv-val">{v}</span>
      {hint && <span className="kv-hint">{hint}</span>}
    </div>
  )
}

function fmtNum(n?: number): string {
  return n ? n.toLocaleString('en-IN', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '—'
}
function fmt2(n?: number): string {
  return n ? n.toFixed(2) : '—'
}
function fmtCr(n?: number): string {
  if (!n) return '—'
  return `₹${Math.round(n / 1e7).toLocaleString('en-IN')}Cr`
}
