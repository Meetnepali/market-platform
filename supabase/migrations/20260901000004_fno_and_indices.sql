-- F&O + indices support: instruments can now be equities, indices,
-- futures, or options (with expiry/strike/underlying), across NSE, BSE
-- and the derivative segments NFO/BFO.
alter table instruments drop constraint instruments_exchange_check;
alter table instruments add constraint instruments_exchange_check
  check (exchange in ('NSE','BSE','NFO','BFO'));

alter table instruments
  add column kind text not null default 'EQ'
    check (kind in ('EQ','INDEX','FUT','CE','PE')),
  add column expiry date,
  add column strike numeric,
  add column underlying text;

create index instruments_fno_idx on instruments (underlying, kind, expiry)
  where kind in ('FUT','CE','PE');

-- Seed the headline indices (streamed via the Yahoo feed adapter).
insert into instruments (exchange, symbol, kind) values
  ('NSE', 'NIFTY 50',   'INDEX'),
  ('NSE', 'NIFTY BANK', 'INDEX'),
  ('NSE', 'NIFTY IT',   'INDEX'),
  ('NSE', 'NIFTY FIN SERVICE', 'INDEX'),
  ('BSE', 'SENSEX',     'INDEX')
on conflict (exchange, symbol) do update set kind = 'INDEX';

-- Stream them: add to the master watchlist.
insert into watchlist_items (watchlist_id, instrument_id)
select w.id, i.id
from watchlists w, instruments i
where w.name = 'NSE + BSE All' and i.kind = 'INDEX'
on conflict do nothing;
