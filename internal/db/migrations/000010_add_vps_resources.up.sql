CREATE TABLE vps_resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vps_id INTEGER NOT NULL,
    resource_type TEXT NOT NULL,
    resource_ocid TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'creating',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (vps_id) REFERENCES vps(id) ON DELETE CASCADE
);
CREATE INDEX idx_vps_resources_vps_type ON vps_resources(vps_id, resource_type);
