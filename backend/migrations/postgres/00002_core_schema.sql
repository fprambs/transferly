-- +goose Up
CREATE TABLE users (
    id uuid PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    locale text NOT NULL DEFAULT 'en',
    timezone text NOT NULL DEFAULT 'UTC',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_vaults (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    kdf_memory_kib integer NOT NULL CHECK (kdf_memory_kib > 0),
    kdf_time integer NOT NULL CHECK (kdf_time > 0),
    kdf_threads integer NOT NULL CHECK (kdf_threads BETWEEN 1 AND 255),
    kdf_salt bytea NOT NULL,
    wrapped_dek bytea NOT NULL,
    recovery_wrapped_dek bytea NOT NULL,
    dek_version integer NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    user_agent text NOT NULL DEFAULT '',
    ip text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX user_sessions_user_id_idx ON user_sessions (user_id);
CREATE INDEX user_sessions_expires_at_idx ON user_sessions (expires_at);

CREATE TABLE workspaces (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('personal', 'team')),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    settings jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workspaces_owner_id_idx ON workspaces (owner_id);

CREATE TABLE memberships (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX memberships_user_id_idx ON memberships (user_id);

CREATE TABLE site_folders (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    parent_id uuid REFERENCES site_folders (id) ON DELETE CASCADE,
    name text NOT NULL
);
CREATE INDEX site_folders_workspace_id_idx ON site_folders (workspace_id);

CREATE TABLE sites (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    folder_id uuid REFERENCES site_folders (id) ON DELETE SET NULL,
    name text NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('sftp', 'ftp', 'ftps')),
    host text NOT NULL,
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    username text NOT NULL,
    auth_method text NOT NULL CHECK (auth_method IN ('password', 'key')),
    list_timezone text NOT NULL DEFAULT 'UTC',
    options jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sites_workspace_id_idx ON sites (workspace_id);

CREATE TABLE site_secrets (
    site_id uuid NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('password', 'private_key', 'passphrase')),
    dek_version integer NOT NULL,
    sealed bytea NOT NULL,
    PRIMARY KEY (site_id, kind)
);

CREATE TABLE known_hosts (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    host text NOT NULL,
    port integer NOT NULL,
    key_type text NOT NULL,
    fingerprint_sha256 text NOT NULL,
    first_seen timestamptz NOT NULL DEFAULT now(),
    accepted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    PRIMARY KEY (workspace_id, host, port, key_type)
);

CREATE TABLE audit_events (
    id bigserial PRIMARY KEY,
    workspace_id uuid REFERENCES workspaces (id) ON DELETE SET NULL,
    user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    action text NOT NULL,
    target text NOT NULL DEFAULT '',
    ip text NOT NULL DEFAULT '',
    meta jsonb NOT NULL DEFAULT '{}',
    at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_workspace_at_idx ON audit_events (workspace_id, at DESC);

-- +goose Down
DROP TABLE audit_events;
DROP TABLE known_hosts;
DROP TABLE site_secrets;
DROP TABLE sites;
DROP TABLE site_folders;
DROP TABLE memberships;
DROP TABLE workspaces;
DROP TABLE user_sessions;
DROP TABLE user_vaults;
DROP TABLE users;
