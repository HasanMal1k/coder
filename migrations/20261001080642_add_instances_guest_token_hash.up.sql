-- SHA-256 of the secret the machine's guest daemon presents when it connects.
ALTER TABLE instances
ADD COLUMN guest_token_hash BYTEA NOT NULL;
