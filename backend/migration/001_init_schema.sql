-- Aibot Backend: Initial Release Schema
-- PostgreSQL 15+ required

CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================
-- 1. users
-- ============================================
CREATE TABLE IF NOT EXISTS users (
    id BIGINT PRIMARY KEY,
    username VARCHAR(100) NOT NULL,
    email VARCHAR(100),
    password_hash VARCHAR(255),
    username_modified BOOLEAN NOT NULL DEFAULT FALSE,
    auth_provider VARCHAR(20) NOT NULL DEFAULT 'local',
    nickname VARCHAR(50),
    avatar_url VARCHAR(255),
    status SMALLINT NOT NULL DEFAULT 1,
    banned_reason VARCHAR(255) NOT NULL DEFAULT '',
    banned_at TIMESTAMPTZ,
    banned_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uni_users_username UNIQUE (username),
    CONSTRAINT uni_users_email UNIQUE (email)
);
CREATE INDEX IF NOT EXISTS idx_user_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_user_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);

-- ============================================
-- 2. oauth_accounts
-- ============================================
CREATE TABLE IF NOT EXISTS oauth_accounts (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    provider VARCHAR(50) NOT NULL,
    provider_uid VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_accounts_user_id ON oauth_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_oauth_accounts_provider ON oauth_accounts(provider);
CREATE INDEX IF NOT EXISTS idx_oauth_accounts_provider_uid ON oauth_accounts(provider_uid);

-- ============================================
-- 3. admin_users
-- ============================================
CREATE TABLE IF NOT EXISTS admin_users (
    id BIGINT PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    nickname VARCHAR(64) NOT NULL,
    role SMALLINT NOT NULL DEFAULT 1,
    status SMALLINT NOT NULL DEFAULT 1,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uni_admin_users_username UNIQUE (username)
);
CREATE INDEX IF NOT EXISTS idx_admin_users_status ON admin_users(status);

-- ============================================
-- 4. admin_sessions
-- ============================================
CREATE TABLE IF NOT EXISTS admin_sessions (
    session_id VARCHAR(64) PRIMARY KEY,
    admin_id BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    client_ip VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(255) NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_admin_id ON admin_sessions(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires_at ON admin_sessions(expires_at);

-- ============================================
-- 5. system_settings
-- ============================================
CREATE TABLE IF NOT EXISTS system_settings (
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================
-- 6. admin_operation_logs
-- ============================================
CREATE TABLE IF NOT EXISTS admin_operation_logs (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(64) NOT NULL,
    target_id VARCHAR(128) NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    client_ip VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_operation_logs_admin_id ON admin_operation_logs(admin_id);
CREATE INDEX IF NOT EXISTS idx_admin_operation_logs_action ON admin_operation_logs(action, created_at DESC);

-- ============================================
-- 7. agents
-- ============================================
CREATE TABLE IF NOT EXISTS agents (
    id BIGINT PRIMARY KEY,
    agent_name VARCHAR(100) NOT NULL,
    model_provider VARCHAR(50),
    system_prompt TEXT,
    avatar_url VARCHAR(255),
    owner_id BIGINT NOT NULL,
    provider_type SMALLINT NOT NULL DEFAULT 1,
    local_endpoint VARCHAR(255) NOT NULL DEFAULT '',
    local_model_name VARCHAR(100) NOT NULL DEFAULT '',
    context_file TEXT NOT NULL DEFAULT '',
    api_key_hash VARCHAR(128) NOT NULL DEFAULT '',
    api_key_hint VARCHAR(16) NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    status SMALLINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_owner_name_active ON agents (owner_id, agent_name)
WHERE status != 3;

-- ============================================
-- 8. sessions
-- ============================================
CREATE TABLE IF NOT EXISTS sessions (
    session_id VARCHAR(50) PRIMARY KEY,
    direct_key VARCHAR(128),
    owner_id BIGINT NOT NULL,
    session_type SMALLINT NOT NULL DEFAULT 1,
    group_name VARCHAR(255) NOT NULL DEFAULT '',
    last_msg_id BIGINT,
    last_msg_summary VARCHAR(255),
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_session_updated ON sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_direct_key ON sessions(direct_key);

-- ============================================
-- 9. session_members
-- ============================================
CREATE TABLE IF NOT EXISTS session_members (
    session_id VARCHAR(50) NOT NULL,
    member_id BIGINT NOT NULL,
    member_type SMALLINT NOT NULL DEFAULT 1,
    custom_title VARCHAR(255) NOT NULL DEFAULT '',
    role SMALLINT NOT NULL DEFAULT 1,
    unread_count INT NOT NULL DEFAULT 0,
    last_read_msg_id BIGINT NOT NULL DEFAULT 0,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, member_id, member_type)
);
CREATE INDEX IF NOT EXISTS idx_member_sessions ON session_members(member_type, member_id, session_id);
CREATE INDEX IF NOT EXISTS idx_member_active ON session_members(member_type, member_id, last_active_at DESC);

-- ============================================
-- 10. messages (HASH partitioned by session_id)
-- ============================================
CREATE TABLE IF NOT EXISTS messages (
    msg_id BIGINT NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    thread_id VARCHAR(255) NOT NULL DEFAULT '',
    sender_id BIGINT NOT NULL,
    sender_type SMALLINT NOT NULL DEFAULT 1,
    msg_type SMALLINT NOT NULL DEFAULT 1,
    content TEXT,
    extra JSONB,
    quoted_message_id BIGINT NOT NULL DEFAULT 0,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (msg_id, session_id)
) PARTITION BY HASH (session_id);
CREATE TABLE IF NOT EXISTS messages_p0 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 0);
CREATE TABLE IF NOT EXISTS messages_p1 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 1);
CREATE TABLE IF NOT EXISTS messages_p2 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 2);
CREATE TABLE IF NOT EXISTS messages_p3 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 3);
CREATE TABLE IF NOT EXISTS messages_p4 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 4);
CREATE TABLE IF NOT EXISTS messages_p5 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 5);
CREATE TABLE IF NOT EXISTS messages_p6 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 6);
CREATE TABLE IF NOT EXISTS messages_p7 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 7);
CREATE TABLE IF NOT EXISTS messages_p8 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 8);
CREATE TABLE IF NOT EXISTS messages_p9 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 9);
CREATE TABLE IF NOT EXISTS messages_p10 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 10);
CREATE TABLE IF NOT EXISTS messages_p11 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 11);
CREATE TABLE IF NOT EXISTS messages_p12 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 12);
CREATE TABLE IF NOT EXISTS messages_p13 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 13);
CREATE TABLE IF NOT EXISTS messages_p14 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 14);
CREATE TABLE IF NOT EXISTS messages_p15 PARTITION OF messages FOR VALUES WITH (MODULUS 16, REMAINDER 15);
CREATE INDEX IF NOT EXISTS idx_msg_session_time ON messages(session_id, msg_id DESC);
CREATE INDEX IF NOT EXISTS idx_msg_active ON messages(session_id, msg_id DESC)
WHERE is_deleted = FALSE;

-- ============================================
-- 11. devices
-- ============================================
CREATE TABLE IF NOT EXISTS devices (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    platform VARCHAR(20) NOT NULL,
    device_token VARCHAR(255) NOT NULL,
    device_id VARCHAR(100),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_devices ON devices(user_id, platform, device_token);

-- ============================================
-- 12. user_inbox
-- ============================================
CREATE TABLE IF NOT EXISTS user_inbox (
    user_id BIGINT NOT NULL,
    inbox_seq BIGINT NOT NULL,
    msg_id BIGINT NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, inbox_seq)
);
CREATE INDEX IF NOT EXISTS idx_inbox_created ON user_inbox(created_at);

-- ============================================
-- 13. llm_usage_logs (RANGE partitioned by month)
-- ============================================
CREATE TABLE IF NOT EXISTS llm_usage_logs (
    id BIGSERIAL NOT NULL,
    user_id BIGINT NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    agent_id BIGINT NOT NULL,
    model_provider VARCHAR(50),
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    is_interrupted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE IF NOT EXISTS llm_usage_logs_2025_01 PARTITION OF llm_usage_logs FOR VALUES FROM (TIMESTAMPTZ '2025-01-01 00:00:00+00') TO (TIMESTAMPTZ '2025-02-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS llm_usage_logs_2025_02 PARTITION OF llm_usage_logs FOR VALUES FROM (TIMESTAMPTZ '2025-02-01 00:00:00+00') TO (TIMESTAMPTZ '2025-03-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS llm_usage_logs_2025_03 PARTITION OF llm_usage_logs FOR VALUES FROM (TIMESTAMPTZ '2025-03-01 00:00:00+00') TO (TIMESTAMPTZ '2025-04-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS llm_usage_logs_default PARTITION OF llm_usage_logs DEFAULT;
CREATE INDEX IF NOT EXISTS idx_usage_user_time ON llm_usage_logs(user_id, created_at DESC);

-- ============================================
-- 14. audit_logs (RANGE partitioned by month)
-- ============================================
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    user_id BIGINT,
    session_id VARCHAR(50),
    msg_id BIGINT,
    detail JSONB,
    client_ip INET,
    user_agent VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
CREATE TABLE IF NOT EXISTS audit_logs_2025_01 PARTITION OF audit_logs FOR VALUES FROM (TIMESTAMPTZ '2025-01-01 00:00:00+00') TO (TIMESTAMPTZ '2025-02-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS audit_logs_2025_02 PARTITION OF audit_logs FOR VALUES FROM (TIMESTAMPTZ '2025-02-01 00:00:00+00') TO (TIMESTAMPTZ '2025-03-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS audit_logs_2025_03 PARTITION OF audit_logs FOR VALUES FROM (TIMESTAMPTZ '2025-03-01 00:00:00+00') TO (TIMESTAMPTZ '2025-04-01 00:00:00+00');
CREATE TABLE IF NOT EXISTS audit_logs_default PARTITION OF audit_logs DEFAULT;
CREATE INDEX IF NOT EXISTS idx_audit_user_time ON audit_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_event ON audit_logs(event_type, created_at DESC);

-- ============================================
-- 15. memory_embeddings (pgvector)
-- ============================================
CREATE TABLE IF NOT EXISTS memory_embeddings (
    id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(50) NOT NULL,
    msg_id BIGINT NOT NULL,
    chunk_index SMALLINT NOT NULL DEFAULT 0,
    content_text TEXT NOT NULL,
    embedding vector(1536),
    embedding_model VARCHAR(50) NOT NULL DEFAULT 'text-embedding-3-small',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_embedding_hnsw ON memory_embeddings USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);
CREATE INDEX IF NOT EXISTS idx_memory_session ON memory_embeddings(session_id);

-- ============================================
-- 16. knowledge_docs (pgvector)
-- ============================================
CREATE TABLE IF NOT EXISTS knowledge_docs (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    doc_title VARCHAR(255),
    chunk_text TEXT NOT NULL,
    embedding vector(1536),
    embedding_model VARCHAR(50) NOT NULL DEFAULT 'text-embedding-3-small',
    source_url VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_knowledge_hnsw ON knowledge_docs USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_knowledge_agent ON knowledge_docs(agent_id);

-- ============================================
-- 17. friend_requests
-- ============================================
CREATE TABLE IF NOT EXISTS friend_requests (
    id BIGINT PRIMARY KEY,
    from_user_id BIGINT NOT NULL,
    to_user_id BIGINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    message VARCHAR(200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_friend_requests_from ON friend_requests(from_user_id);
CREATE INDEX IF NOT EXISTS idx_friend_requests_to ON friend_requests(to_user_id);

-- ============================================
-- 18. friends
-- ============================================
CREATE TABLE IF NOT EXISTS friends (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    friend_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_friend ON friends(user_id, friend_id);

-- ============================================
-- 19. delegation_logs
-- ============================================
CREATE TABLE IF NOT EXISTS delegation_logs (
    id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    agent_id BIGINT NOT NULL,
    action VARCHAR(20) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_delegation_logs_session ON delegation_logs(session_id, created_at);

-- ============================================
-- 20. message_reactions
-- ============================================
CREATE TABLE IF NOT EXISTS message_reactions (
    id BIGSERIAL PRIMARY KEY,
    msg_id BIGINT NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    user_id BIGINT NOT NULL,
    emoji VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reaction_unique ON message_reactions(msg_id, session_id, user_id, emoji);
CREATE INDEX IF NOT EXISTS idx_reaction_msg ON message_reactions(msg_id, session_id);

-- ============================================
-- 21. auth_refresh_tokens
-- ============================================
CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
    jti VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    family_id VARCHAR(64) NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    parent_jti VARCHAR(64),
    replaced_by_jti VARCHAR(64),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refresh_user_family ON auth_refresh_tokens(user_id, family_id);
CREATE INDEX IF NOT EXISTS idx_refresh_family_status ON auth_refresh_tokens(family_id, status);
CREATE INDEX IF NOT EXISTS idx_refresh_expires_at ON auth_refresh_tokens(expires_at);

-- ============================================
-- 22. session_history_resets
-- ============================================
CREATE TABLE IF NOT EXISTS session_history_resets (
    session_id VARCHAR(50) NOT NULL,
    user_id BIGINT NOT NULL,
    deleted_before TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_session_history_resets_user ON session_history_resets(user_id);
