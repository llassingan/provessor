ALTER TABLE networks ADD COLUMN provisioning_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE vps ADD COLUMN provisioning_state TEXT NOT NULL DEFAULT 'pending';
