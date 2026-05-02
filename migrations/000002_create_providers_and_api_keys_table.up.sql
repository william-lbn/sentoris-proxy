-- Create providers table for LLM provider configuration
CREATE TABLE IF NOT EXISTS providers (
    name TEXT PRIMARY KEY,
    base_url TEXT NOT NULL,
    auth_header TEXT NOT NULL,
    models JSONB NOT NULL,
    input_price_per_1k DECIMAL(10,6) NOT NULL DEFAULT 0,
    output_price_per_1k DECIMAL(10,6) NOT NULL DEFAULT 0,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create api_keys table for API key management
-- Keys are stored encrypted using AES-GCM
CREATE TABLE IF NOT EXISTS api_keys (
    id SERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NULL,
    expires_at TIMESTAMP NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    permissions JSONB NOT NULL DEFAULT '["read", "write"]',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP NULL
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_providers_is_default ON providers(is_default);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_prefix ON api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_is_active ON api_keys(is_active);
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);