-- Forum boards, topics, posts, moderators, search.

CREATE TABLE IF NOT EXISTS boards (
    id INTEGER PRIMARY KEY,
    parent_id INTEGER REFERENCES boards(id) ON DELETE RESTRICT,
    slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
    name TEXT NOT NULL,
    description TEXT,
    visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'members')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'hidden')),
    topic_count INTEGER NOT NULL DEFAULT 0,
    reply_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS boards_parent_idx ON boards(parent_id, sort_order);

CREATE TABLE IF NOT EXISTS topics (
    id INTEGER PRIMARY KEY,
    board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE RESTRICT,
    title TEXT NOT NULL,
    slug TEXT COLLATE NOCASE,
    author_user_id INTEGER NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'locked', 'deleted')),
    pinned INTEGER NOT NULL DEFAULT 0,
    reply_count INTEGER NOT NULL DEFAULT 0,
    view_count INTEGER NOT NULL DEFAULT 0,
    last_post_at INTEGER,
    last_post_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS topics_board_idx ON topics(board_id, pinned DESC, last_post_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS topics_author_idx ON topics(author_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS forum_posts (
    id INTEGER PRIMARY KEY,
    topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    author_user_id INTEGER NOT NULL REFERENCES users(id),
    quote_post_id INTEGER REFERENCES forum_posts(id) ON DELETE SET NULL,
    body_md TEXT NOT NULL,
    body_html TEXT NOT NULL,
    floor_no INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible', 'pending', 'hidden', 'deleted')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(topic_id, floor_no)
);
CREATE INDEX IF NOT EXISTS forum_posts_topic_idx ON forum_posts(topic_id, floor_no);
CREATE INDEX IF NOT EXISTS forum_posts_author_idx ON forum_posts(author_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS forum_moderators (
    board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    granted_by_admin_id INTEGER REFERENCES admin_users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(board_id, user_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS topics_fts USING fts5(
    topic_id UNINDEXED,
    title,
    body,
    tokenize = 'unicode61'
);
CREATE VIRTUAL TABLE IF NOT EXISTS forum_posts_fts USING fts5(
    post_id UNINDEXED,
    topic_id UNINDEXED,
    body,
    tokenize = 'unicode61'
);
