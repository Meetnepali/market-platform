-- Retention: keep the DB bounded forever.
--   signals:    keep 90 days (audit history), delete older
--   candles_1m: keep 30 days of minute bars; roll older into candles_1d
create extension if not exists pg_cron;

-- Daily candles built from expiring minute bars
create table if not exists candles_1d (
  instrument_id bigint not null references instruments (id),
  candle_time   date not null,
  open numeric, high numeric, low numeric, close numeric, volume bigint,
  primary key (instrument_id, candle_time)
);
alter table candles_1d enable row level security;
create policy "public read daily candles" on candles_1d for select using (true);

create or replace function public.run_retention()
returns void language plpgsql security definer set search_path = public as $$
begin
  -- 1. Roll minute bars older than 30 days into daily bars
  insert into candles_1d (instrument_id, candle_time, open, high, low, close, volume)
  select instrument_id,
         (candle_time at time zone 'Asia/Kolkata')::date,
         (array_agg(open  order by candle_time asc))[1],
         max(high),
         min(low),
         (array_agg(close order by candle_time desc))[1],
         max(volume)
  from candles_1m
  where candle_time < now() - interval '30 days'
  group by instrument_id, (candle_time at time zone 'Asia/Kolkata')::date
  on conflict (instrument_id, candle_time) do nothing;

  -- 2. Delete the rolled-up minute bars
  delete from candles_1m where candle_time < now() - interval '30 days';

  -- 3. Expire old signals
  delete from signals where created_at < now() - interval '90 days';
end;
$$;

-- Run nightly at 01:00 UTC (06:30 IST, before market open)
select cron.schedule('nightly-retention', '0 1 * * *', 'select public.run_retention()');
