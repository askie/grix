-- Phase 1: 通话记录表
-- 对应架构文档 §11.1

CREATE TABLE IF NOT EXISTS call_records (
    id                   BIGINT PRIMARY KEY,
    session_id           VARCHAR(50)  NOT NULL,
    caller_id            BIGINT       NOT NULL,
    callee_id            BIGINT       NOT NULL,
    call_mode            SMALLINT     NOT NULL DEFAULT 1,   -- 1=voice
    state                SMALLINT     NOT NULL DEFAULT 0,   -- 0=ringing,1=active,2=ended,3=rejected,4=missed,5=error
    delegation_mode      TEXT         NOT NULL DEFAULT 'human',
    delegated_agent_id   BIGINT,
    ai_provider          TEXT,
    handover_events      JSONB,
    started_at           TIMESTAMPTZ,
    answered_at          TIMESTAMPTZ,
    ended_at             TIMESTAMPTZ,
    duration_seconds     INT,
    end_reason           TEXT,
    recording_caller_url TEXT,
    recording_callee_url TEXT,
    recording_ai_url     TEXT,
    recording_mixed_url  TEXT,
    transcript_full_url  TEXT,
    segment_count        INT          NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_call_records_session ON call_records(session_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_call_records_caller  ON call_records(caller_id,  started_at DESC);
CREATE INDEX IF NOT EXISTS idx_call_records_callee  ON call_records(callee_id,  started_at DESC);
