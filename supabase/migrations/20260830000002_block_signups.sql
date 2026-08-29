-- Single-user project: block creation of any auth account other than the
-- allowed one. Already applied to the remote project directly.
create or replace function public.block_unauthorized_signups()
returns trigger
language plpgsql
security definer
set search_path = ''
as $$
begin
  if new.email is distinct from 'meetnepali922@gmail.com' then
    raise exception 'Signups are disabled on this project';
  end if;
  return new;
end;
$$;

drop trigger if exists block_unauthorized_signups on auth.users;
create trigger block_unauthorized_signups
  before insert on auth.users
  for each row execute function public.block_unauthorized_signups();
