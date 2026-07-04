CREATE TABLE network_resources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    resource_type TEXT NOT NULL,
    resource_ocid TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'creating',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE
);
CREATE INDEX idx_network_resources_network_type ON network_resources(network_id, resource_type);
