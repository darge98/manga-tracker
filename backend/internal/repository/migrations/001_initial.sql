CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS users (
 id text PRIMARY KEY,
 username text NOT NULL CHECK (octet_length(username) BETWEEN 2 AND 50),
 email text NOT NULL UNIQUE CHECK (email = lower(email)),
 password_hash text NOT NULL CHECK (password_hash <> '')
);
CREATE TABLE IF NOT EXISTS manga (
 id integer PRIMARY KEY CHECK (id > 0),
 metadata jsonb NOT NULL CHECK (jsonb_typeof(metadata) = 'object' AND metadata ? 'id' AND metadata ? 'title' AND jsonb_typeof(metadata->'id') = 'number' AND jsonb_typeof(metadata->'title') = 'string' AND (metadata->>'id')::integer = id AND length(trim(metadata->>'title')) > 0)
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS library (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 manga_id integer NOT NULL REFERENCES manga(id),
 to_read boolean NOT NULL DEFAULT false,
 favorite boolean NOT NULL DEFAULT false,
 PRIMARY KEY (user_id,manga_id)
);
CREATE TABLE IF NOT EXISTS chapters (
 user_id text NOT NULL,
 manga_id integer NOT NULL,
 chapter numeric NOT NULL CHECK (chapter > 0 AND chapter <= 100000),
 is_read boolean NOT NULL,
 read_at timestamptz,
 rating smallint CHECK (rating BETWEEN 1 AND 5),
 notes text NOT NULL DEFAULT '' CHECK (octet_length(notes) <= 5000),
 PRIMARY KEY (user_id,manga_id,chapter),
 FOREIGN KEY (user_id,manga_id) REFERENCES library(user_id,manga_id) ON DELETE CASCADE,
 CHECK ((is_read AND read_at IS NOT NULL) OR (NOT is_read AND read_at IS NULL))
);
CREATE INDEX IF NOT EXISTS chapters_recent_idx ON chapters(user_id,read_at DESC) WHERE is_read;
CREATE TABLE IF NOT EXISTS lists (
 id text PRIMARY KEY,
 owner_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 100),
 description text NOT NULL DEFAULT '' CHECK (octet_length(description) <= 1000),
 is_public boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS lists_owner_idx ON lists(owner_id,created_at);
CREATE TABLE IF NOT EXISTS list_items (
 list_id text NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
 manga_id integer NOT NULL REFERENCES manga(id),
 position integer NOT NULL CHECK (position >= 0),
 PRIMARY KEY (list_id,manga_id),
 UNIQUE (list_id,position)
);
INSERT INTO schema_migrations(version) VALUES (1) ON CONFLICT DO NOTHING;
