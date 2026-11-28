CREATE TABLE staff (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL CHECK (length(display_name) <= 80),
  colour TEXT NOT NULL DEFAULT '' CHECK (colour = '' OR colour GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'),
  linked_user_id TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  pseudo_no INTEGER,
  created_at TEXT NOT NULL,
  archived_at TEXT,
  CHECK (status = 'active' OR pseudo_no IS NOT NULL));
CREATE TABLE entries (
  id TEXT PRIMARY KEY,
  origin_till_id TEXT NOT NULL,
  author_user_id TEXT NOT NULL,
  staff_id TEXT,
  business_date TEXT NOT NULL CHECK (business_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
  currency TEXT NOT NULL CHECK (length(currency) = 3),
  payment_type TEXT NOT NULL CHECK (payment_type IN ('cash','card')),
  total_minor INTEGER NOT NULL CHECK (total_minor >= 0),
  tip_minor INTEGER NOT NULL DEFAULT 0 CHECK (tip_minor >= 0),
  sale_ref TEXT,
  supersedes_id TEXT,
  voided INTEGER NOT NULL DEFAULT 0 CHECK (voided IN (0,1)),
  shadowed INTEGER NOT NULL DEFAULT 0 CHECK (shadowed IN (0,1)),
  fwd_state TEXT NOT NULL DEFAULT 'na' CHECK (fwd_state IN ('na','pending','queued')),
  mode TEXT NOT NULL DEFAULT 'free' CHECK (mode IN ('free','linked')),
  created_at TEXT NOT NULL);
CREATE TABLE entry_lines (
  entry_id TEXT NOT NULL,
  line_no INTEGER NOT NULL,
  item_id TEXT,
  name TEXT NOT NULL CHECK (length(name) <= 120),
  qty INTEGER NOT NULL CHECK (qty BETWEEN 1 AND 999),
  amount_minor INTEGER NOT NULL CHECK (amount_minor >= 0),
  description TEXT CHECK (description IS NULL OR length(description) <= 200),
  staff_id TEXT,
  PRIMARY KEY (entry_id, line_no));
CREATE TABLE meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE INDEX idx_entries_date ON entries (business_date, staff_id);
CREATE INDEX idx_entries_supersedes ON entries (supersedes_id);
CREATE INDEX idx_entries_saleref ON entries (sale_ref);
CREATE INDEX idx_entries_fwd ON entries (fwd_state) WHERE fwd_state = 'pending';
