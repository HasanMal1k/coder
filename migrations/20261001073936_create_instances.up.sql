CREATE TABLE instances (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('creating', 'stopped', 'starting', 'running', 'stopping', 'deleting', 'error')),
    provider TEXT NOT NULL CHECK (provider IN ('docker', 'fly')),
    machine_id TEXT,
    volume_id TEXT,
    cpus INT NOT NULL CHECK (cpus > 0),
    memory_mb INT NOT NULL CHECK (memory_mb > 0),
    disk_gb INT NOT NULL CHECK (disk_gb > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Also serves "all instances for this user": owner_id is the leading column.
    UNIQUE (owner_id, name)
);
