import { supabase } from './supabase'

const API_URL = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

export interface Instrument {
  id: number
  exchange: 'NSE' | 'BSE'
  symbol: string
  active: boolean
}

export interface Quote {
  instrument: string
  exchange: 'NSE' | 'BSE'
  ltp: number
  open: number
  high: number
  low: number
  previous_close: number
  volume: number
  week_ago_close?: number
  event_time: string
}

export interface Signal {
  id: string
  strategy_id: string
  symbol: string
  signal_type: string
  price: number
  metrics: Record<string, number>
  created_at: string
}

export interface StockDetails {
  symbol: string
  name?: string
  exchange: string
  fifty_two_week_high?: number
  fifty_two_week_low?: number
  market_cap?: number
  trailing_pe?: number
  price_to_book?: number
  trailing_eps?: number
  dividend_yield?: number
  book_value?: number
  debt_to_equity?: number
  roe?: number
  beta?: number
  alpha_annual?: number
  vol_annual?: number
  year_return?: number
}

export interface Candle {
  time: string
  open: number
  high: number
  low: number
  close: number
  volume: number
}

async function accessToken(): Promise<string> {
  const { data } = await supabase.auth.getSession()
  const token = data.session?.access_token
  if (!token) throw new Error('Not signed in')
  return token
}

async function get<T>(path: string): Promise<T> {
  const token = await accessToken()
  const res = await fetch(`${API_URL}${path}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!res.ok) throw new Error(`${res.status}: ${await res.text()}`)
  return res.json()
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const token = await accessToken()
  const res = await fetch(`${API_URL}${path}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status}: ${await res.text()}`)
  return res.json()
}

export const api = {
  instruments: (): Promise<Instrument[]> =>
    fetch(`${API_URL}/api/instruments`).then((r) => r.json()),
  quote: (symbol: string) => get<Quote>(`/api/quotes/${symbol}`),
  stockDetails: (symbol: string, exchange = 'NSE') =>
    get<StockDetails>(`/api/stocks/${symbol}?exchange=${exchange}`),
  candles: (symbol: string, limit = 500) =>
    get<Candle[]>(`/api/candles/${symbol}?limit=${limit}`),
  signals: (limit = 100) => get<Signal[]>(`/api/signals?limit=${limit}`),
  createStrategy: (body: {
    name: string
    configuration: unknown
    instrument_ids: number[]
  }) => post<{ id: string }>('/api/strategies', body),
  enableStrategy: (id: string) => post(`/api/strategies/${id}/enable`),
  disableStrategy: (id: string) => post(`/api/strategies/${id}/disable`),

  /** Live quotes via SSE. Returns a cleanup function. */
  streamQuotes: async (
    symbols: string[],
    onQuote: (q: Quote) => void,
  ): Promise<() => void> => {
    const token = await accessToken()
    const url = `${API_URL}/ws/quotes?symbols=${symbols.join(',')}&token=${token}`
    const es = new EventSource(url)
    es.onmessage = (ev) => onQuote(JSON.parse(ev.data) as Quote)
    return () => es.close()
  },
}
