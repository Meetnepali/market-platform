import { createClient } from '@supabase/supabase-js'

// Browser-side Supabase client: anon key only. RLS is what protects
// data — the anon key is safe to ship, the service key never is.
export const supabase = createClient(
  import.meta.env.VITE_SUPABASE_URL,
  import.meta.env.VITE_SUPABASE_ANON_KEY,
)
