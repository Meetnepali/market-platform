-- Initial schema: instrument master, strategies, signals, candles,
-- watchlists. RLS on every user-owned table; backend workers connect
-- with the service role and bypass RLS by design.

-- ── Reference data ──────────────────────────────────────────────────
create table instruments (
  id             bigint generated always as identity primary key,
  exchange       text not null check (exchange in ('NSE', 'BSE')),
  symbol         text not null,
  provider_token bigint,
  tick_size      numeric,
  lot_size       integer,
  active         boolean not null default true,
  unique (exchange, symbol)
);

-- ── Profiles (mirror of auth.users for app joins) ───────────────────
create table profiles (
  id         uuid primary key references auth.users (id) on delete cascade,
  email      text,
  created_at timestamptz not null default now()
);

create or replace function public.handle_new_user()
returns trigger
language plpgsql security definer set search_path = public as $$
begin
  insert into public.profiles (id, email) values (new.id, new.email);
  return new;
end;
$$;

create trigger on_auth_user_created
  after insert on auth.users
  for each row execute function public.handle_new_user();

-- ── Strategies ──────────────────────────────────────────────────────
create table strategies (
  id                 uuid primary key default gen_random_uuid(),
  user_id            uuid not null references auth.users (id) on delete cascade,
  name               text not null,
  enabled            boolean not null default false,
  configuration_json jsonb not null default '{}',
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now()
);
create index strategies_user_idx on strategies (user_id);
create index strategies_enabled_idx on strategies (enabled) where enabled;

create table strategy_instruments (
  strategy_id   uuid not null references strategies (id) on delete cascade,
  instrument_id bigint not null references instruments (id) on delete cascade,
  primary key (strategy_id, instrument_id)
);

-- ── Signals ─────────────────────────────────────────────────────────
create table signals (
  id            uuid primary key default gen_random_uuid(),
  strategy_id   uuid not null references strategies (id) on delete cascade,
  instrument_id bigint not null references instruments (id),
  signal_type   text not null,
  price         numeric,
  metrics_json  jsonb not null default '{}',
  created_at    timestamptz not null default now()
);
create index signals_strategy_time_idx on signals (strategy_id, created_at desc);

-- ── Candles ─────────────────────────────────────────────────────────
create table candles_1m (
  instrument_id bigint not null references instruments (id),
  candle_time   timestamptz not null,
  open   numeric,
  high   numeric,
  low    numeric,
  close  numeric,
  volume bigint,
  primary key (instrument_id, candle_time)
);

-- ── Watchlists ──────────────────────────────────────────────────────
create table watchlists (
  id      uuid primary key default gen_random_uuid(),
  user_id uuid not null references auth.users (id) on delete cascade,
  name    text not null
);
create index watchlists_user_idx on watchlists (user_id);

create table watchlist_items (
  watchlist_id  uuid not null references watchlists (id) on delete cascade,
  instrument_id bigint not null references instruments (id) on delete cascade,
  primary key (watchlist_id, instrument_id)
);

-- ── Row Level Security ──────────────────────────────────────────────
alter table instruments enable row level security;
create policy "public read instruments" on instruments
  for select using (true);

alter table profiles enable row level security;
create policy "own profile" on profiles
  for select using (auth.uid() = id);

alter table strategies enable row level security;
create policy "own strategies" on strategies
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);

alter table strategy_instruments enable row level security;
create policy "own strategy instruments" on strategy_instruments
  for all using (
    exists (select 1 from strategies s
            where s.id = strategy_instruments.strategy_id
              and s.user_id = auth.uid())
  );

alter table signals enable row level security;
create policy "signals via owned strategy" on signals
  for select using (
    exists (select 1 from strategies s
            where s.id = signals.strategy_id and s.user_id = auth.uid())
  );

alter table candles_1m enable row level security;
create policy "public read candles" on candles_1m
  for select using (true);

alter table watchlists enable row level security;
create policy "own watchlists" on watchlists
  for all using (auth.uid() = user_id) with check (auth.uid() = user_id);

alter table watchlist_items enable row level security;
create policy "own watchlist items" on watchlist_items
  for all using (
    exists (select 1 from watchlists w
            where w.id = watchlist_items.watchlist_id
              and w.user_id = auth.uid())
  );

-- ── Realtime: broadcast signal inserts to the owning browser ────────
alter publication supabase_realtime add table signals;
