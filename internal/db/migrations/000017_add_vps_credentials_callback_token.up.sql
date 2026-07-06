ALTER TABLE vps ADD COLUMN credentials_callback_token_hash TEXT;
ALTER TABLE vps ADD COLUMN credentials_callback_token_expires_at DATETIME;
ALTER TABLE vps ADD COLUMN credentials_callback_token_used_at DATETIME;
ALTER TABLE vps ADD COLUMN credentials_received_at DATETIME;
