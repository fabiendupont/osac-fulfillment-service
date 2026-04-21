CREATE TABLE IF NOT EXISTS compute_instance_groups (
    id TEXT NOT NULL PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name TEXT,
    creation_timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    finalizers TEXT[],
    creators TEXT[],
    tenants TEXT[],
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    version BIGINT NOT NULL DEFAULT 0,
    data JSONB NOT NULL DEFAULT '{}'
);

CREATE UNIQUE INDEX IF NOT EXISTS compute_instance_groups_name ON compute_instance_groups (name) WHERE deletion_timestamp IS NULL;
