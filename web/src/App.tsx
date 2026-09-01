import { memo, useEffect, useMemo, useRef, useState } from 'react'
import type { Session } from '@supabase/supabase-js'
import { supabase } from './lib/supabase'
import { api, type Quote, type Signal } from './lib/api'
import { useSignals } from './hooks/useSignals'
import { useQuotes } from './hooks/useQuotes'
import { StockDetail } from './components/StockDetail'
import { FnoTab } from './components/FnoTab'

const ALL = ['*'] // stream every stock the backend tracks
const ALLOWED_EMAIL = 'meetnepali922@gmail.com'

export default function App() {
  const [session, setSession] = useState<Session | null>(null)

  useEffect(() => {
    supabase.auth.getSession().then(({ data }) => setSession(data.session))
    const { data: sub } = supabase.auth.onAuthStateChange((_e, s) => setSession(s))
    return () => sub.subscription.unsubscribe()
  }, [])

  // Belt and braces on top of the DB-side signup trigger: any session that
  // isn't the allowed account gets dropped immediately.
  useEffect(() => {
    if (session && session.user.email !== ALLOWED_EMAIL) supabase.auth.signOut()
  }, [session])

  if (!session || session.user.email !== ALLOWED_EMAIL) return <SignIn />
  return <Dashboard email={session.user.email ?? ''} />
}

function SignIn() {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const signIn = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    const { error } = await supabase.auth.signInWithPassword({ email, password })
    setBusy(false)
    if (error) setError('Invalid email or password')
  }

  return (
    <main className="signin">
      <div className="signin-card">
        <div className="logo-mark">▲▼</div>
        <h1>Market Platform</h1>
        <p className="tagline">Live NSE/BSE Signals &amp; Streaming Quotes</p>
        <form onSubmit={signIn}>
          <input
            type="email"
            required
            autoComplete="email"
            placeholder="Email address"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
          <input
            type="password"
            required
            autoComplete="current-password"
            placeholder="Password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <button type="submit" className="btn-primary" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign In'}
          </button>
          {error && <p className="error-msg">{error}</p>}
        </form>
      </div>
    </main>
  )
}

const MAX_ROWS = 300

type Tab = 'market' | 'fno' | 'buy' | 'sell' | 'stats'

const TABS: { key: Tab; label: string }[] = [
  { key: 'market', label: 'Market Watchlist' },
  { key: 'fno', label: 'F&O' },
  { key: 'buy', label: 'Buy Signals' },
  { key: 'sell', label: 'Sell Signals' },
  { key: 'stats', label: 'Weekly Stats' },
]

const INDICES = ['NIFTY 50', 'NIFTY BANK', 'NIFTY IT', 'NIFTY FIN SERVICE', 'SENSEX']

function Dashboard({ email }: { email: string }) {
  const [symbolById, setSymbolById] = useState<Record<number, string>>({})
  useEffect(() => {
    api
      .instruments()
      .then((list) =>
        setSymbolById(Object.fromEntries(list.map((i) => [i.id, i.symbol]))),
      )
      .catch(console.error)
  }, [])

  const quotes = useQuotes(ALL)
  const signals = useSignals(symbolById)
  const [tab, setTab] = useState<Tab>('market')

  const buySignals = useMemo(
    () => signals.filter((s) => s.signal_type.startsWith('BUY')),
    [signals],
  )
  const sellSignals = useMemo(
    () => signals.filter((s) => s.signal_type.startsWith('SELL')),
    [signals],
  )

  return (
    <main className="dashboard">
      <header>
        <div className="brand-wrapper">
          <h1>Market Platform</h1>
          <span className="live-badge">
            <span className="live-dot" aria-hidden />
            Live
          </span>
        </div>
        <div className="user-nav">
          <span className="who">{email}</span>
          <button className="btn-signout" onClick={() => supabase.auth.signOut()}>
            Sign Out
          </button>
        </div>
      </header>

      <IndicesStrip quotes={quotes} />

      <nav className="tabs">
        {TABS.map((t) => (
          <button
            key={t.key}
            className={tab === t.key ? 'tab active' : 'tab'}
            onClick={() => setTab(t.key)}
          >
            {t.label}
            {t.key === 'buy' && buySignals.length > 0 && (
              <span className="pill buy-pill">{buySignals.length}</span>
            )}
            {t.key === 'sell' && sellSignals.length > 0 && (
              <span className="pill sell-pill">{sellSignals.length}</span>
            )}
          </button>
        ))}
      </nav>

      {tab === 'market' && <MarketTab quotes={quotes} />}
      {tab === 'fno' && <FnoTab quotes={quotes} />}
      {tab === 'buy' && (
        <SignalTab
          title="Buy Signals"
          hint="Stocks down ≥20% over the last week — oversold dip candidates."
          signals={buySignals}
          tone="up"
        />
      )}
      {tab === 'sell' && (
        <SignalTab
          title="Sell Signals"
          hint="Stocks up ≥20% over the last week — overbought, consider booking profit."
          signals={sellSignals}
          tone="down"
        />
      )}
      {tab === 'stats' && <StatsTab quotes={quotes} />}
    </main>
  )
}

/* ── Indices strip (always visible) ───────────────────────────────── */

function IndicesStrip({ quotes }: { quotes: Record<string, Quote> }) {
  const available = INDICES.map((sym) => {
    const q = quotes[`NSE:${sym}`] ?? quotes[`BSE:${sym}`]
    return q ? { sym, q } : null
  }).filter(Boolean) as { sym: string; q: Quote }[]

  if (available.length === 0) return null
  return (
    <div className="indices-strip">
      {available.map(({ sym, q }) => {
        const chg = q.previous_close
          ? ((q.ltp - q.previous_close) / q.previous_close) * 100
          : 0
        return (
          <div key={sym} className="index-card">
            <span className="index-name">{sym}</span>
            <b>{q.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}</b>
            <span className={chg >= 0 ? 'up' : 'down'}>
              {chg >= 0 ? '+' : ''}
              {chg.toFixed(2)}%
            </span>
          </div>
        )
      })}
    </div>
  )
}

/* ── Market tab (the streaming watchlist) ─────────────────────────── */

function MarketTab({ quotes }: { quotes: Record<string, Quote> }) {
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<'symbol' | 'change' | 'week'>('change')
  const [selected, setSelected] = useState<Quote | null>(null)

  const rows = useMemo(() => {
    const q = Object.values(quotes)
    const needle = search.trim().toUpperCase()
    const filtered = needle ? q.filter((x) => x.instrument.includes(needle)) : q
    filtered.sort((a, b) =>
      sortBy === 'symbol'
        ? a.instrument.localeCompare(b.instrument)
        : sortBy === 'week'
          ? weekPct(b) - weekPct(a)
          : changePct(b) - changePct(a),
    )
    return filtered
  }, [quotes, search, sortBy])

  const visible = rows.slice(0, MAX_ROWS)

  return (
    <section>
      <h2>
        <span>Live Watchlist</span>
        <span className="count">
          {rows.length.toLocaleString('en-IN')} stocks streaming
          {rows.length > MAX_ROWS && ` · showing top ${MAX_ROWS}`}
        </span>
      </h2>

      <div className="toolbar">
        <div className="search-box">
          <span className="search-icon">🔍</span>
          <input
            type="search"
            placeholder="Search stock symbol… (e.g. RELIANCE, TCS)"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <div className="filter-group">
          <button
            className={`filter-btn ${sortBy === 'change' ? 'active' : ''}`}
            onClick={() => setSortBy('change')}
          >
            Day Movers
          </button>
          <button
            className={`filter-btn ${sortBy === 'week' ? 'active' : ''}`}
            onClick={() => setSortBy('week')}
          >
            1W Movers
          </button>
          <button
            className={`filter-btn ${sortBy === 'symbol' ? 'active' : ''}`}
            onClick={() => setSortBy('symbol')}
          >
            A–Z
          </button>
        </div>
      </div>

      <div className="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Stock</th>
              <th className="text-right">LTP (₹)</th>
              <th className="text-right">Day Change</th>
              <th className="text-right">1W Move</th>
              <th className="text-right">Day Range (L – H)</th>
              <th className="text-right">Volume</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((q) => (
              <QuoteRow key={`${q.exchange}:${q.instrument}`} q={q} onSelect={setSelected} />
            ))}
          </tbody>
        </table>
      </div>

      {rows.length === 0 && (
        <div className="empty-state">
          <p>Waiting for live price updates from feed…</p>
        </div>
      )}

      {selected && <StockDetail quote={selected} onClose={() => setSelected(null)} />}
    </section>
  )
}

/* ── Buy / Sell signal tabs ───────────────────────────────────────── */

function SignalTab({
  title,
  hint,
  signals,
  tone,
}: {
  title: string
  hint: string
  signals: Signal[]
  tone: 'up' | 'down'
}) {
  return (
    <section>
      <h2>
        <span>{title}</span>
        <span className="count">{signals.length} generated</span>
      </h2>
      <p className="hint-text">{hint}</p>

      {signals.length === 0 ? (
        <div className="empty-state">
          <p>No signals active yet — incoming signals will trigger and display here immediately.</p>
        </div>
      ) : (
        <div className="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Time</th>
                <th>Stock</th>
                <th>Signal</th>
                <th className="text-right">Trigger Price</th>
                <th className="text-right">1W Move</th>
              </tr>
            </thead>
            <tbody>
              {signals.slice(0, 200).map((s) => (
                <tr key={s.id} className="signal-row">
                  <td className="num-muted">
                    {new Date(s.created_at).toLocaleTimeString('en-IN', {
                      hour: '2-digit',
                      minute: '2-digit',
                      second: '2-digit',
                    })}
                  </td>
                  <td>
                    <span className="stock-symbol">{s.symbol}</span>
                  </td>
                  <td>
                    <span className="signal-badge">{s.signal_type}</span>
                  </td>
                  <td className="text-right price-text">
                    {s.price != null ? `₹${Number(s.price).toLocaleString('en-IN', { minimumFractionDigits: 2 })}` : '—'}
                  </td>
                  <td className="text-right">
                    {s.metrics?.week_change_percent != null ? (
                      <span className={`chg-pill ${tone}`}>
                        <span className="arrow">{Number(s.metrics.week_change_percent) >= 0 ? '▲' : '▼'}</span>
                        {Math.abs(Number(s.metrics.week_change_percent)).toFixed(2)}%
                      </span>
                    ) : (
                      <span className="num-muted">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/* ── Weekly stats tab ─────────────────────────────────────────────── */

function StatsTab({ quotes }: { quotes: Record<string, Quote> }) {
  const stats = useMemo(() => {
    const all = Object.values(quotes).filter((q) => (q.week_ago_close ?? 0) > 0)
    const bucket = (lo: number, hi: number) =>
      all.filter((q) => weekPct(q) >= lo && weekPct(q) < hi).length
    const losers = [...all].sort((a, b) => weekPct(a) - weekPct(b)).slice(0, 10)
    const gainers = [...all].sort((a, b) => weekPct(b) - weekPct(a)).slice(0, 10)
    return {
      total: all.length,
      down20: all.filter((q) => weekPct(q) <= -20).length,
      down10: bucket(-20, -10),
      down5: bucket(-10, -5),
      flat: bucket(-5, 5),
      up5: bucket(5, 10),
      up10: bucket(10, 20),
      up20: all.filter((q) => weekPct(q) >= 20).length,
      losers,
      gainers,
    }
  }, [quotes])

  if (stats.total === 0) {
    return (
      <div className="empty-state">
        <p>Weekly data is still loading — values will populate as price history aggregates.</p>
      </div>
    )
  }

  return (
    <section>
      <h2>
        <span>Market Breadth &amp; Distribution</span>
        <span className="count">{stats.total.toLocaleString('en-IN')} stocks tracked</span>
      </h2>

      <div className="stat-grid">
        <div className="stat-card down">
          <b>{stats.down20}</b>
          <span>Down &gt; 20%</span>
        </div>
        <div className="stat-card down">
          <b>{stats.down10}</b>
          <span>Down 10–20%</span>
        </div>
        <div className="stat-card down">
          <b>{stats.down5}</b>
          <span>Down 5–10%</span>
        </div>
        <div className="stat-card">
          <b>{stats.flat}</b>
          <span>±5% Neutral</span>
        </div>
        <div className="stat-card up">
          <b>{stats.up5}</b>
          <span>Up 5–10%</span>
        </div>
        <div className="stat-card up">
          <b>{stats.up10}</b>
          <span>Up 10–20%</span>
        </div>
        <div className="stat-card up">
          <b>{stats.up20}</b>
          <span>Up &gt; 20%</span>
        </div>
      </div>

      <div className="two-col">
        <div>
          <h2>Top 10 Weekly Losers</h2>
          <MoversTable rows={stats.losers} />
        </div>
        <div>
          <h2>Top 10 Weekly Gainers</h2>
          <MoversTable rows={stats.gainers} />
        </div>
      </div>
    </section>
  )
}

function MoversTable({ rows }: { rows: Quote[] }) {
  return (
    <div className="table-wrapper">
      <table>
        <thead>
          <tr>
            <th>Stock</th>
            <th className="text-right">LTP</th>
            <th className="text-right">1W Move</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((q) => {
            const w = weekPct(q)
            const isUp = w >= 0
            return (
              <tr key={`${q.exchange}:${q.instrument}`}>
                <td>
                  <div className="stock-name-cell">
                    <span className="stock-symbol">{q.instrument}</span>
                    <span className="exch-badge">{q.exchange}</span>
                  </div>
                </td>
                <td className="text-right price-text">
                  ₹{q.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}
                </td>
                <td className="text-right">
                  <span className={`chg-pill ${isUp ? 'up' : 'down'}`}>
                    <span className="arrow">{isUp ? '▲' : '▼'}</span>
                    {Math.abs(w).toFixed(2)}%
                  </span>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ── Shared helper functions & QuoteRow ───────────────────────────── */

function changePct(q: Quote): number {
  return q.previous_close ? ((q.ltp - q.previous_close) / q.previous_close) * 100 : 0
}

function weekPct(q: Quote): number {
  const base = q.week_ago_close ?? 0
  return base > 0 ? ((q.ltp - base) / base) * 100 : 0
}

function fmtCurrency(val: number): string {
  return val.toLocaleString('en-IN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function fmtVolume(vol: number): string {
  if (vol >= 10000000) {
    return `${(vol / 10000000).toFixed(2)} Cr`
  }
  if (vol >= 100000) {
    return `${(vol / 100000).toFixed(2)} L`
  }
  return vol.toLocaleString('en-IN')
}

const QuoteRow = memo(function QuoteRow({
  q,
  onSelect,
}: {
  q: Quote
  onSelect?: (q: Quote) => void
}) {
  const chg = changePct(q)
  const wk = weekPct(q)
  const isDayUp = chg >= 0
  const isWkUp = wk >= 0
  const prev = useRef(q.ltp)
  const dir = q.ltp > prev.current ? 'tick-up' : q.ltp < prev.current ? 'tick-down' : ''

  useEffect(() => {
    prev.current = q.ltp
  }, [q.ltp])

  return (
    <tr className={onSelect ? 'clickable' : undefined} onClick={() => onSelect?.(q)}>
      <td>
        <div className="stock-name-cell">
          <span className="stock-symbol">{q.instrument}</span>
          <span className="exch-badge">{q.exchange}</span>
        </div>
      </td>
      <td className="text-right">
        <span key={q.ltp} className={`ltp price-text ${dir}`}>
          ₹{fmtCurrency(q.ltp)}
        </span>
      </td>
      <td className="text-right">
        <span className={`chg-pill ${isDayUp ? 'up' : 'down'}`}>
          <span className="arrow">{isDayUp ? '▲' : '▼'}</span>
          {Math.abs(chg).toFixed(2)}%
        </span>
      </td>
      <td className="text-right">
        {q.week_ago_close ? (
          <span className={`chg-pill ${isWkUp ? 'up' : 'down'}`}>
            <span className="arrow">{isWkUp ? '▲' : '▼'}</span>
            {Math.abs(wk).toFixed(2)}%
          </span>
        ) : (
          <span className="num-muted">—</span>
        )}
      </td>
      <td className="text-right num-muted">
        {q.low && q.high ? `${fmtCurrency(q.low)} – ${fmtCurrency(q.high)}` : '—'}
      </td>
      <td className="text-right num-muted">{fmtVolume(q.volume)}</td>
    </tr>
  )
})
