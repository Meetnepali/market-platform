import { memo, useEffect, useMemo, useRef, useState } from 'react'
import type { Session } from '@supabase/supabase-js'
import { supabase } from './lib/supabase'
import { api, type Quote, type Signal } from './lib/api'
import { useSignals } from './hooks/useSignals'
import { useQuotes } from './hooks/useQuotes'

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
        <p className="tagline">Live NSE/BSE signals &amp; streaming quotes</p>
        <form onSubmit={signIn}>
          <input
            type="email"
            required
            autoComplete="email"
            placeholder="Email"
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
          <button type="submit" className="primary" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
          {error && <p className="error">{error}</p>}
        </form>
      </div>
    </main>
  )
}

const MAX_ROWS = 300

type Tab = 'market' | 'buy' | 'sell' | 'stats'

const TABS: { key: Tab; label: string }[] = [
  { key: 'market', label: 'Market' },
  { key: 'buy', label: 'Buy signals' },
  { key: 'sell', label: 'Sell signals' },
  { key: 'stats', label: 'Weekly stats' },
]

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
        <h1>
          <span className="live-dot" aria-hidden />
          Live Market
        </h1>
        <div>
          <span className="who">{email}</span>
          <button onClick={() => supabase.auth.signOut()}>Sign out</button>
        </div>
      </header>

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
      {tab === 'buy' && (
        <SignalTab
          title="Buy signals"
          hint="Stocks down ≥20% over the last week — oversold dip candidates."
          signals={buySignals}
          tone="up"
        />
      )}
      {tab === 'sell' && (
        <SignalTab
          title="Sell signals"
          hint="Stocks up ≥20% over the last week — overbought, consider booking profit."
          signals={sellSignals}
          tone="down"
        />
      )}
      {tab === 'stats' && <StatsTab quotes={quotes} />}
    </main>
  )
}

/* ── Market tab (the streaming watchlist) ─────────────────────────── */

function MarketTab({ quotes }: { quotes: Record<string, Quote> }) {
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<'symbol' | 'change' | 'week'>('change')

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
        Watchlist{' '}
        <span className="count">
          {rows.length.toLocaleString('en-IN')} stocks streaming
          {rows.length > MAX_ROWS && ` · showing top ${MAX_ROWS} — search to narrow`}
        </span>
      </h2>
      <div className="toolbar">
        <input
          type="search"
          placeholder="Search symbol… e.g. TATA"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button className={sortBy === 'change' ? 'active' : ''} onClick={() => setSortBy('change')}>
          Day movers
        </button>
        <button className={sortBy === 'week' ? 'active' : ''} onClick={() => setSortBy('week')}>
          Week movers
        </button>
        <button className={sortBy === 'symbol' ? 'active' : ''} onClick={() => setSortBy('symbol')}>
          A–Z
        </button>
      </div>
      <table>
        <thead>
          <tr><th>Symbol</th><th>Exch</th><th>LTP</th><th>Day</th><th>1W</th><th>Day range</th><th>Volume</th></tr>
        </thead>
        <tbody>
          {visible.map((q) => (
            <QuoteRow key={`${q.exchange}:${q.instrument}`} q={q} />
          ))}
        </tbody>
      </table>
      {rows.length === 0 && <p className="empty">Waiting for the first price sweep…</p>}
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
        {title} <span className="count">{signals.length}</span>
      </h2>
      <p className="empty">{hint}</p>
      {signals.length === 0 ? (
        <p className="empty">Nothing yet — signals land here the moment the rule fires.</p>
      ) : (
        <table>
          <thead>
            <tr><th>Time</th><th>Symbol</th><th>Signal</th><th>Price</th><th>1W move</th></tr>
          </thead>
          <tbody>
            {signals.slice(0, 200).map((s) => (
              <tr key={s.id} className="signal-row">
                <td>{new Date(s.created_at).toLocaleTimeString('en-IN')}</td>
                <td className="sym">{s.symbol}</td>
                <td><span className="badge">{s.signal_type}</span></td>
                <td>{s.price != null ? `₹${Number(s.price).toFixed(2)}` : '—'}</td>
                <td className={tone}>
                  {s.metrics?.week_change_percent != null
                    ? `${Number(s.metrics.week_change_percent) >= 0 ? '+' : ''}${Number(s.metrics.week_change_percent).toFixed(2)}%`
                    : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
    return <p className="empty">Weekly data is still loading — it fills in as the sweep progresses.</p>
  }

  return (
    <section>
      <h2>
        This week across the market{' '}
        <span className="count">{stats.total.toLocaleString('en-IN')} stocks with weekly data</span>
      </h2>

      <div className="stat-grid">
        <div className="stat-card down"><b>{stats.down20}</b><span>fell &gt; 20%</span></div>
        <div className="stat-card down"><b>{stats.down10}</b><span>fell 10–20%</span></div>
        <div className="stat-card down"><b>{stats.down5}</b><span>fell 5–10%</span></div>
        <div className="stat-card"><b>{stats.flat}</b><span>±5% flat</span></div>
        <div className="stat-card up"><b>{stats.up5}</b><span>rose 5–10%</span></div>
        <div className="stat-card up"><b>{stats.up10}</b><span>rose 10–20%</span></div>
        <div className="stat-card up"><b>{stats.up20}</b><span>rose &gt; 20%</span></div>
      </div>

      <div className="two-col">
        <div>
          <h2>Top weekly losers</h2>
          <MoversTable rows={stats.losers} />
        </div>
        <div>
          <h2>Top weekly gainers</h2>
          <MoversTable rows={stats.gainers} />
        </div>
      </div>
    </section>
  )
}

function MoversTable({ rows }: { rows: Quote[] }) {
  return (
    <table>
      <thead>
        <tr><th>Symbol</th><th>LTP</th><th>1W</th></tr>
      </thead>
      <tbody>
        {rows.map((q) => {
          const w = weekPct(q)
          return (
            <tr key={`${q.exchange}:${q.instrument}`}>
              <td className="sym">{q.instrument} <span className="exch">{q.exchange}</span></td>
              <td>₹{q.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}</td>
              <td className={w >= 0 ? 'up' : 'down'}>{`${w >= 0 ? '+' : ''}${w.toFixed(2)}%`}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

/* ── Shared bits ──────────────────────────────────────────────────── */

function changePct(q: Quote): number {
  return q.previous_close ? ((q.ltp - q.previous_close) / q.previous_close) * 100 : 0
}

function weekPct(q: Quote): number {
  const base = q.week_ago_close ?? 0
  return base > 0 ? ((q.ltp - base) / base) * 100 : 0
}

const QuoteRow = memo(function QuoteRow({ q }: { q: Quote }) {
  const chg = changePct(q)
  const wk = weekPct(q)
  const prev = useRef(q.ltp)
  const dir = q.ltp > prev.current ? 'tick-up' : q.ltp < prev.current ? 'tick-down' : ''
  useEffect(() => {
    prev.current = q.ltp
  }, [q.ltp])
  return (
    <tr>
      <td className="sym">{q.instrument}</td>
      <td className="exch">{q.exchange}</td>
      <td>
        {/* keying on ltp restarts the flash animation on every price change */}
        <span key={q.ltp} className={`ltp ${dir}`}>
          ₹{q.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}
        </span>
      </td>
      <td className={chg >= 0 ? 'up' : 'down'}>
        {`${chg >= 0 ? '+' : ''}${chg.toFixed(2)}%`}
      </td>
      <td className={wk >= 0 ? 'up' : 'down'}>
        {q.week_ago_close ? `${wk >= 0 ? '+' : ''}${wk.toFixed(2)}%` : '—'}
      </td>
      <td>{q.low && q.high ? `${q.low.toFixed(2)} – ${q.high.toFixed(2)}` : '—'}</td>
      <td>{q.volume.toLocaleString('en-IN')}</td>
    </tr>
  )
})
