import { useEffect, useState } from 'react'
import { api, type Signal } from '../lib/api'
import { supabase } from '../lib/supabase'

/**
 * Live signal feed: initial page from the Go API, then realtime inserts
 * pushed by Supabase Realtime. RLS guarantees this browser only ever
 * receives rows belonging to the signed-in user's strategies.
 */
export function useSignals(symbolById: Record<number, string>): Signal[] {
  const [signals, setSignals] = useState<Signal[]>([])

  useEffect(() => {
    let cancelled = false

    api.signals().then((initial) => {
      if (!cancelled) setSignals(initial)
    }).catch(console.error)

    const channel = supabase
      .channel('signals-feed')
      .on(
        'postgres_changes',
        { event: 'INSERT', schema: 'public', table: 'signals' },
        (payload) => {
          const row = payload.new as {
            id: string
            strategy_id: string
            instrument_id: number
            signal_type: string
            price: number
            metrics_json: Record<string, number>
            created_at: string
          }
          setSignals((prev) => {
            if (prev.some((s) => s.id === row.id)) return prev
            return [
              {
                id: row.id,
                strategy_id: row.strategy_id,
                symbol: symbolById[row.instrument_id] ?? `#${row.instrument_id}`,
                signal_type: row.signal_type,
                price: row.price,
                metrics: row.metrics_json,
                created_at: row.created_at,
              },
              ...prev,
            ]
          })
        },
      )
      .subscribe()

    return () => {
      cancelled = true
      supabase.removeChannel(channel)
    }
  }, [Object.keys(symbolById).length])

  return signals
}
