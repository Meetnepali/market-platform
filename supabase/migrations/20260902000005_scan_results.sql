-- EOD buy-scan results: the ranked "pullback in an uptrend" picks the
-- scanner worker produces once per trading day. One row per pick;
-- re-running a scan replaces that day's rows.

create table scan_results (
  id            uuid primary key default gen_random_uuid(),
  scan_date     date not null,
  instrument_id bigint not null references instruments (id),
  symbol        text not null,
  rank          integer not null,
  score         numeric not null,
  close         numeric not null,
  reasons_json  jsonb not null default '[]',
  metrics_json  jsonb not null default '{}',
  created_at    timestamptz not null default now(),
  unique (scan_date, instrument_id)
);

create index scan_results_date_rank_idx on scan_results (scan_date desc, rank);

-- Reference-style data: any signed-in user may read; only the backend
-- service role writes.
alter table scan_results enable row level security;
create policy "authenticated read scan results" on scan_results
  for select to authenticated using (true);
