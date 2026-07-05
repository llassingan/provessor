CREATE TABLE settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    tenancy_ocid    TEXT NOT NULL,
    user_ocid       TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    private_key     TEXT NOT NULL,
    region          TEXT NOT NULL,
    compartment_ocid TEXT NOT NULL,
    vcn_ocid        TEXT NOT NULL,
    subnet_ocid     TEXT NOT NULL,
    api_base_url    TEXT NOT NULL DEFAULT '',
    api_token       TEXT NOT NULL,
    network_provisioned INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO settings (id, tenancy_ocid, user_ocid, fingerprint, private_key, region, compartment_ocid, vcn_ocid, subnet_ocid, api_base_url, api_token, network_provisioned)
VALUES (1, '', '', '', '', '', '', '', '', '', hex(randomblob(32)), 0);
