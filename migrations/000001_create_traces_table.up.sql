-- Create traces table
CREATE TABLE IF NOT EXISTS traces (
    trace_id TEXT PRIMARY KEY,
    parent_id TEXT NULL,
    session_id TEXT NOT NULL,
    execution_state TEXT NOT NULL,
    model TEXT NOT NULL,
    input JSONB NOT NULL,
    output JSONB NOT NULL,
    observations JSONB NOT NULL,
    proofs JSONB NOT NULL,
    constraints_applied JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    ttl_expire_at TIMESTAMP NULL,
    extensions JSONB NOT NULL DEFAULT '{}'
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_traces_session_id ON traces(session_id);
CREATE INDEX IF NOT EXISTS idx_traces_parent_id ON traces(parent_id);
CREATE INDEX IF NOT EXISTS idx_traces_created_at ON traces(created_at);
CREATE INDEX IF NOT EXISTS idx_traces_ttl_expire_at ON traces(ttl_expire_at);
