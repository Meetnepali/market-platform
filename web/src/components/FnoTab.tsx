import { useEffect, useMemo, useState } from 'react'
import { api, type FnoContract, type FnoContracts, type FnoUnderlying, type Quote } from '../lib/api'
import { PriceChart } from './PriceChart'
import { DataTable, type Column } from './DataTable'

interface StrikeRow {
  strike: number
  ce?: string
  pe?: string
  isAtm: boolean
}

/**
 * F&O tab: browse the real derivative contract master (futures +
 * option strike ladders per expiry) with a chart of the underlying.
 * Live premiums/OI need the Kite feed — surfaced honestly until then.
 */
export function FnoTab({ quotes }: { quotes: Record<string, Quote> }) {
  const [underlyings, setUnderlyings] = useState<FnoUnderlying[]>([])
  const [selected, setSelected] = useState('NIFTY')
  const [contracts, setContracts] = useState<FnoContracts | null>(null)
  const [expiry, setExpiry] = useState('')

  useEffect(() => {
    api.fnoUnderlyings().then(setUnderlyings).catch(console.error)
  }, [])

  useEffect(() => {
    setContracts(null)
    api
      .fnoContracts(selected)
      .then((c) => {
        setContracts(c)
        const expiries = Object.keys(c.options_by_expiry).sort()
        setExpiry(expiries[0] ?? '')
      })
      .catch(console.error)
  }, [selected])

  // Spot price: indices stream as e.g. "NIFTY 50"; stocks as-is.
  const spotSymbol = selected === 'NIFTY' ? 'NIFTY 50' : selected === 'BANKNIFTY' ? 'NIFTY BANK' : selected
  const spot = quotes[`NSE:${spotSymbol}`]

  const chain: StrikeRow[] = useMemo(() => {
    if (!contracts || !expiry) return []
    const rows = contracts.options_by_expiry[expiry] ?? []
    const byStrike = new Map<number, { ce?: string; pe?: string }>()
    for (const c of rows) {
      const e = byStrike.get(c.strike ?? 0) ?? {}
      if (c.kind === 'CE') e.ce = c.symbol
      else e.pe = c.symbol
      byStrike.set(c.strike ?? 0, e)
    }
    let strikes = [...byStrike.entries()].sort((a, b) => a[0] - b[0])
    // trim to ±12 strikes around spot when we know it
    let atm = -1
    if (spot) {
      atm = 0
      for (let i = 1; i < strikes.length; i++) {
        if (Math.abs(strikes[i][0] - spot.ltp) < Math.abs(strikes[atm][0] - spot.ltp)) atm = i
      }
      if (strikes.length > 25) {
        const lo = Math.max(0, atm - 12)
        strikes = strikes.slice(lo, atm + 13)
        atm -= lo
      }
    }
    return strikes.map(([strike, e], i) => ({ strike, ce: e.ce, pe: e.pe, isAtm: i === atm }))
  }, [contracts, expiry, spot])

  const expiries = contracts ? Object.keys(contracts.options_by_expiry).sort() : []

  return (
    <section>
      <div className="toolbar">
        <select value={selected} onChange={(e) => setSelected(e.target.value)}>
          {underlyings.map((u) => (
            <option key={u.underlying} value={u.underlying}>
              {u.underlying} ({u.options} opts)
            </option>
          ))}
        </select>
        {spot && (
          <span className="spot">
            Spot <b>₹{spot.ltp.toLocaleString('en-IN', { minimumFractionDigits: 2 })}</b>
          </span>
        )}
      </div>

      <PriceChart symbol={spotSymbol} exchange="NSE" />

      <h2>Futures</h2>
      {!contracts ? (
        <p className="empty">Loading contracts…</p>
      ) : (
        <DataTable
          columns={futuresColumns}
          rows={contracts.futures}
          rowKey={(f) => f.symbol}
          maxHeight="30vh"
          emptyMessage="No futures contracts for this underlying."
        />
      )}

      <h2>
        Option chain{' '}
        {expiries.length > 0 && (
          <select value={expiry} onChange={(e) => setExpiry(e.target.value)}>
            {expiries.map((e) => (
              <option key={e} value={e}>{e}</option>
            ))}
          </select>
        )}
      </h2>
      {chain.length === 0 ? (
        <p className="empty">No option contracts for this underlying.</p>
      ) : (
        <>
          <DataTable
            className="chain"
            columns={chainColumns}
            rows={chain}
            rowKey={(r) => String(r.strike)}
            rowClassName={(r) => (r.isAtm ? 'atm' : undefined)}
            maxHeight="55vh"
          />
          <p className="empty">
            Contract structure is live from the exchange master (strikes, expiries, lot sizes).
            Premiums, OI and IV stream in once the Kite Connect feed (₹500/mo) is enabled — no
            free API provides live Indian F&O prices.
          </p>
        </>
      )}
    </section>
  )
}

const futuresColumns: Column<FnoContract>[] = [
  { key: 'contract', header: 'Contract', render: (f) => <span className="sym">{f.symbol}</span> },
  { key: 'expiry', header: 'Expiry', render: (f) => f.expiry },
  { key: 'lot', header: 'Lot size', render: (f) => f.lot_size },
  { key: 'price', header: 'Price', render: () => <span className="empty">live with Kite feed</span> },
]

const chainColumns: Column<StrikeRow>[] = [
  { key: 'ce', header: 'Call (CE)', render: (r) => r.ce ?? '—' },
  {
    key: 'strike',
    header: 'Strike',
    align: 'center',
    width: '120px',
    render: (r) => <b>{r.strike.toLocaleString('en-IN')}</b>,
  },
  { key: 'pe', header: 'Put (PE)', render: (r) => r.pe ?? '—' },
]
