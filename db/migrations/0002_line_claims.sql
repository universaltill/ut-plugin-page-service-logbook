ALTER TABLE entry_lines ADD COLUMN sale_ref TEXT;
ALTER TABLE entry_lines ADD COLUMN created_at TEXT;
CREATE UNIQUE INDEX idx_lines_sale_line ON entry_lines (sale_ref, line_no) WHERE sale_ref IS NOT NULL;
