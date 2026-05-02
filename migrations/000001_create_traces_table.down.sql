-- Drop indexes
DROP INDEX IF EXISTS idx_traces_ttl_expire_at;
DROP INDEX IF EXISTS idx_traces_created_at;
DROP INDEX IF EXISTS idx_traces_parent_id;
DROP INDEX IF EXISTS idx_traces_session_id;

-- Drop traces table
DROP TABLE IF EXISTS traces;
