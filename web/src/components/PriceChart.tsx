import { useEffect, useRef, useState } from 'react'
import { createChart, type IChartApi, type UTCTimestamp } from 'lightweight-charts'
import { api } from '../lib/api'

const RANGES = [
  { key: '5d', interval: '15m', label: '1W' },
  { key: '1mo', interval: '1h', label: '1M' },
  { key: '3mo', interval: '1d', label: '3M' },
  { key: '1y', interval: '1d', label: '1Y' },
  { key: '5y', interval: '1wk', label: '5Y' },
]

/** Candlestick chart fed by the backend's Yahoo history proxy. */
export function PriceChart({ symbol, exchange }: { symbol: string; exchange: string }) {
  const el = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const [range, setRange] = useState(RANGES[2])
  const [error, setError] = useState('')

  useEffect(() => {
    if (!el.current) return
    const styles = getComputedStyle(el.current)
    const chart = createChart(el.current, {
      height: 240,
      layout: {
        background: { color: 'transparent' },
        textColor: styles.color,
      },
      grid: {
        vertLines: { color: 'rgba(128,128,128,0.08)' },
        horzLines: { color: 'rgba(128,128,128,0.08)' },
      },
      rightPriceScale: { borderVisible: false },
      timeScale: { borderVisible: false },
    })
    const series = chart.addCandlestickSeries({
      upColor: '#16a34a', downColor: '#dc2626',
      wickUpColor: '#16a34a', wickDownColor: '#dc2626',
      borderVisible: false,
    })
    chartRef.current = chart

    let cancelled = false
    setError('')
    api
      .history(symbol, exchange, range.key, range.interval)
      .then((bars) => {
        if (cancelled) return
        series.setData(
          bars.map((b) => ({
            time: b.time as UTCTimestamp,
            open: b.open, high: b.high, low: b.low, close: b.close,
          })),
        )
        chart.timeScale().fitContent()
      })
      .catch(() => !cancelled && setError('Chart data unavailable'))

    const onResize = () => el.current && chart.applyOptions({ width: el.current.clientWidth })
    onResize()
    window.addEventListener('resize', onResize)
    return () => {
      cancelled = true
      window.removeEventListener('resize', onResize)
      chart.remove()
      chartRef.current = null
    }
  }, [symbol, exchange, range])

  return (
    <div className="price-chart">
      <div className="chart-ranges">
        {RANGES.map((r) => (
          <button
            key={r.label}
            className={r.label === range.label ? 'active' : ''}
            onClick={() => setRange(r)}
          >
            {r.label}
          </button>
        ))}
      </div>
      <div ref={el} />
      {error && <p className="empty">{error}</p>}
    </div>
  )
}
