import { useEffect, useRef, useState } from 'react'
import { api, type Quote } from '../lib/api'

/**
 * Live quotes via the Go API's SSE stream.
 * Pass ['*'] to stream every stock the backend is tracking.
 *
 * Incoming events are buffered and flushed to React state twice a
 * second — thousands of individual setState calls would jank the UI.
 */
export function useQuotes(symbols: string[]): Record<string, Quote> {
  const [quotes, setQuotes] = useState<Record<string, Quote>>({})
  const buffer = useRef<Record<string, Quote>>({})

  useEffect(() => {
    if (symbols.length === 0) return
    let cleanup: (() => void) | undefined
    let cancelled = false

    api
      .streamQuotes(symbols, (q) => {
        // Key by exchange+symbol: RELIANCE trades on both NSE and BSE.
        buffer.current[`${q.exchange}:${q.instrument}`] = q
      })
      .then((stop) => {
        if (cancelled) stop()
        else cleanup = stop
      })

    const flush = setInterval(() => {
      if (Object.keys(buffer.current).length === 0) return
      const pending = buffer.current
      buffer.current = {}
      setQuotes((prev) => ({ ...prev, ...pending }))
    }, 500)

    return () => {
      cancelled = true
      cleanup?.()
      clearInterval(flush)
    }
  }, [symbols.join(',')])

  return quotes
}
