-- Downloads, comments, friend links, redirects.

CREATE TABLE IF NOT EXISTS entry_attachments (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    download_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS entry_attachments_entry_idx ON entry_attachments(entry_id, sort_order);

CREATE TABLE IF NOT EXISTS comments (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    author_name TEXT,
    author_email TEXT,
    parent_id INTEGER REFERENCES comments(id) ON DELETE SET NULL,
    body_md TEXT NOT NULL,
    body_html TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'spam', 'deleted')),
    ip TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_entry_idx ON comments(entry_id, status, created_at);
CREATE INDEX IF NOT EXISTS comments_status_idx ON comments(status, created_at DESC);

CREATE TABLE IF NOT EXISTS friend_links (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    logo_media_id INTEGER REFERENCES media(id) ON DELETE SET NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS friend_links_enabled_idx ON friend_links(enabled, sort_order);

CREATE TABLE IF NOT EXISTS redirects (
    id INTEGER PRIMARY KEY,
    from_path TEXT NOT NULL UNIQUE,
    to_path TEXT NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 301 CHECK (status_code IN (301, 302)),
    created_at INTEGER NOT NULL
);
