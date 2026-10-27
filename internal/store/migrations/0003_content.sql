-- Content model, media, revisions.

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY,
    parent_id INTEGER REFERENCES channels(id) ON DELETE RESTRICT,
    slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('article', 'product', 'case', 'job', 'download')),
    description TEXT,
    list_template TEXT,
    detail_template TEXT,
    show_in_nav INTEGER NOT NULL DEFAULT 1,
    comments_enabled INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'hidden')),
    seo_title TEXT,
    seo_description TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS channels_parent_idx ON channels(parent_id, sort_order);

CREATE TABLE IF NOT EXISTS entries (
    id INTEGER PRIMARY KEY,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
    slug TEXT COLLATE NOCASE,
    title TEXT NOT NULL,
    summary TEXT,
    body_md TEXT NOT NULL DEFAULT '',
    body_html TEXT NOT NULL DEFAULT '',
    cover_media_id INTEGER,
    author_admin_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    pinned INTEGER NOT NULL DEFAULT 0,
    featured INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    view_count INTEGER NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL DEFAULT '{}',
    published_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER,
    UNIQUE(channel_id, slug)
);
CREATE INDEX IF NOT EXISTS entries_channel_idx ON entries(channel_id, status, published_at DESC);
CREATE INDEX IF NOT EXISTS entries_featured_idx ON entries(featured, status);
CREATE INDEX IF NOT EXISTS entries_deleted_idx ON entries(deleted_at);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS entry_tags (
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);

CREATE TABLE IF NOT EXISTS pages (
    id INTEGER PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
    title TEXT NOT NULL,
    body_md TEXT NOT NULL DEFAULT '',
    body_html TEXT NOT NULL DEFAULT '',
    template TEXT NOT NULL DEFAULT 'page',
    show_in_nav INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft', 'published', 'archived')),
    seo_title TEXT,
    seo_description TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER
);
CREATE INDEX IF NOT EXISTS pages_status_idx ON pages(status, deleted_at);

CREATE TABLE IF NOT EXISTS banners (
    id INTEGER PRIMARY KEY,
    title TEXT,
    subtitle TEXT,
    image_media_id INTEGER,
    link_url TEXT,
    button_text TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('image', 'document')),
    stored_name TEXT NOT NULL,
    original_name TEXT NOT NULL,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL,
    width INTEGER,
    height INTEGER,
    sha256 TEXT NOT NULL,
    uploader_type TEXT NOT NULL CHECK (uploader_type IN ('admin', 'user')),
    uploader_id INTEGER,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    deleted_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS media_sha_idx ON media(sha256);
CREATE INDEX IF NOT EXISTS media_status_idx ON media(status, created_at DESC);

CREATE TABLE IF NOT EXISTS media_variants (
    id INTEGER PRIMARY KEY,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    stored_name TEXT NOT NULL,
    width INTEGER,
    height INTEGER,
    size INTEGER,
    UNIQUE(media_id, label)
);

CREATE TABLE IF NOT EXISTS entry_media (
    id INTEGER PRIMARY KEY,
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL DEFAULT 'gallery' CHECK (kind IN ('gallery', 'inline')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    caption TEXT
);
CREATE INDEX IF NOT EXISTS entry_media_entry_idx ON entry_media(entry_id, sort_order);

CREATE TABLE IF NOT EXISTS content_revisions (
    id INTEGER PRIMARY KEY,
    target_type TEXT NOT NULL CHECK (target_type IN ('entry', 'page')),
    target_id INTEGER NOT NULL,
    title TEXT,
    body_md TEXT,
    body_html TEXT,
    editor_admin_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS content_revisions_target_idx ON content_revisions(target_type, target_id, created_at DESC);
