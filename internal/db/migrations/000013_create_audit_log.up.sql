CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id INTEGER,
    provider TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);
