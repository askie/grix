CREATE TABLE IF NOT EXISTS user_favorite_paths (
    id          BIGINT PRIMARY KEY NOT NULL,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    path        TEXT NOT NULL,
    name        VARCHAR(255) NOT NULL,
    is_directory BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_favorite_paths_user_id ON user_favorite_paths(user_id);
CREATE UNIQUE INDEX idx_user_favorite_paths_user_path ON user_favorite_paths(user_id, path);
