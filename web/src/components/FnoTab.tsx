import { useEffect, useMemo, useState } from 'react'
import { api, type FnoContracts, type FnoUnderlying, type Quote } from '../lib/api'
import { PriceChart } from './PriceChart'

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

  const chain = useMemo(() => {
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
    if (spot && strikes.length > 25) {
      let atm = 0
      for (let i = 1; i < strikes.length; i++) {
        if (Math.abs(strikes[i][0] - spot.ltp) < Math.abs(strikes[atm][0] - spot.ltp)) atm = i
      }
      strikes = strikes.slice(Math.max(0, atm - 12), atm + 13)
    }
    return strikes
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
        <table>
          <thead>
            <tr><th>Contract</th><th>Expiry</th><th>Lot size</th><th>Price</th></tr>
          </thead>
          <tbody>
            {contracts.futures.map((f) => (
              <tr key={f.symbol}>
                <td className="sym">{f.symbol}</td>
                <td>{f.expiry}</td>
                <td>{f.lot_size}</td>
                <td className="empty">live with Kite feed</td>
              </tr>
            ))}
          </tbody>
        </table>
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
          <table className="chain">
            <thead>
              <tr><th>Call (CE)</th><th className="strike-col">Strike</th><th>Put (PE)</th></tr>
            </thead>
            <tbody>
              {chain.map(([strike, e]) => {
                const isAtm = spot && Math.abs(strike - spot.ltp) === Math.min(...chain.map(([s]) => Math.abs(s - spot.ltp)))
                return (
                  <tr key={strike} className={isAtm ? 'atm' : undefined}>
                    <td>{e.ce ?? '—'}</td>
                    <td className="strike-col"><b>{strike.toLocaleString('en-IN')}</b></td>
                    <td>{e.pe ?? '—'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
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
