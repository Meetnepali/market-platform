import { useEffect, useState } from 'react'
import { api, type ScanPick, type ScanResponse } from '../lib/api'
import { DataTable, type Column } from './DataTable'

/** Buy Signals tab: the EOD scanner's ranked "pullback in an uptrend"
 *  picks — strong stocks that just dipped while selling pressure fades.
 *  Refreshed once per trading day by the scanner worker. */
export function BuyScanTab() {
  const [scan, setScan] = useState<ScanResponse | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .latestScan()
      .then(setScan)
      .catch(() => setError('Could not load scan results'))
  }, [])

  return (
    <section>
      <h2>
        <span>Buy Signals</span>
        <span className="count">
          {scan?.scan_date
            ? `${scan.picks.length} picks · scanned ${scan.scan_date}`
            : 'loading…'}
        </span>
      </h2>
      <p className="hint-text">
        Strong stocks in an uptrend that just pulled back while selling
        volume dries up — ranked by bounce-back score. Not investment
        advice; diversify across picks.
      </p>

      {error && <p className="error-msg">{error}</p>}

      <DataTable
        columns={columns}
        rows={scan?.picks ?? []}
        rowKey={(p) => p.symbol}
        rowClassName={() => 'signal-row'}
        emptyMessage="No picks yet — the scanner runs after market close; results appear once a scan qualifies stocks."
        maxHeight="65vh"
      />
    </section>
  )
}

const columns: Column<ScanPick>[] = [
  {
    key: 'rank',
    header: '#',
    render: (p) => <span className="num-muted">{p.rank}</span>,
  },
  {
    key: 'stock',
    header: 'Stock',
    render: (p) => <span className="stock-symbol">{p.symbol}</span>,
  },
  {
    key: 'score',
    header: 'Score',
    align: 'right',
    render: (p) => (
      <span className={`chg-pill ${p.score >= 60 ? 'up' : ''}`}>
        {p.score.toFixed(0)}/100
      </span>
    ),
  },
  {
    key: 'close',
    header: 'Close (₹)',
    align: 'right',
    render: (p) => (
      <span className="price-text">
        ₹{p.close.toLocaleString('en-IN', { minimumFractionDigits: 2 })}
      </span>
    ),
  },
  {
    key: 'pullback',
    header: 'Pullback',
    align: 'right',
    render: (p) => (
      <span className="num-muted">
        {p.metrics.pullback_pct != null ? `−${p.metrics.pullback_pct.toFixed(1)}%` : '—'}
      </span>
    ),
  },
  {
    key: 'rsi2',
    header: 'RSI(2)',
    align: 'right',
    render: (p) => (
      <span className="num-muted">
        {p.metrics.rsi2 != null ? p.metrics.rsi2.toFixed(0) : '—'}
      </span>
    ),
  },
  {
    key: 'reasons',
    header: 'Why it qualifies',
    render: (p) => (
      <span className="reasons-cell">
        {p.reasons.map((r) => (
          <span key={r} className="reason-chip">
            {r}
          </span>
        ))}
      </span>
    ),
  },
]
