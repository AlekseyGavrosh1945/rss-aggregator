CREATE TABLE users (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chat_id    BIGINT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE feeds (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    url             TEXT NOT NULL UNIQUE,
    title           TEXT NOT NULL DEFAULT '',
    last_fetched_at TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subscriptions (
    user_id    BIGINT NOT NULL REFERENCES users ON DELETE CASCADE,
    feed_id    BIGINT NOT NULL REFERENCES feeds ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, feed_id)
);

CREATE TABLE posts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feed_id      BIGINT NOT NULL REFERENCES feeds ON DELETE CASCADE,
    guid         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    link         TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (feed_id, guid)
);

CREATE INDEX idx_posts_feed_published ON posts (feed_id, published_at DESC);
CREATE INDEX idx_subscriptions_feed ON subscriptions (feed_id);
