-- Site engagement, banners, contact messages, full-text search.

CREATE TABLE IF NOT EXISTS contact_messages (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    phone TEXT,
    email TEXT,
    company TEXT,
    subject TEXT,
    message TEXT NOT NULL,
    source_page TEXT,
    ip TEXT,
    user_agent TEXT,
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'read', 'replied', 'spam')),
    admin_note TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS contact_messages_status_idx ON contact_messages(status, created_at DESC);
CREATE INDEX IF NOT EXISTS banners_enabled_idx ON banners(enabled, sort_order);

-- FTS5 tables hold CJK-normalized plain text. The application keeps them in
-- sync on publish/update/delete; a LIKE fallback is used if FTS is unavailable.
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
    entry_id UNINDEXED,
    title,
    summary,
    body,
    tokenize = 'unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    page_id UNINDEXED,
    title,
    body,
    tokenize = 'unicode61'
);
