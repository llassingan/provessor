INSERT OR IGNORE INTO templates (id, name, description, type, cloud_init_yaml, shape, default_ocpu, default_memory, boot_volume_size_gb, created_at)
VALUES (
  100,
  'Ubuntu Basic',
  'Vanilla Ubuntu with UFW, fail2ban, and unattended-upgrades',
  'predefined',
  '#cloud-config
ansible:
  package_name: ansible-core
  install_method: distro
  pull:
    url: https://github.com/llassingan/provessor.git
    playbook_names: [ansible/templates/ubuntu/playbook.yml]
    clean: true
    full: true

write_files:
  - path: /etc/ssh/sshd_config.d/50-provessor.conf
    permissions: ''0644''
    content: |
      PermitRootLogin prohibit-password

runcmd:
  - apt-get update
  - apt-get install -y fail2ban
  - systemctl enable fail2ban
  - systemctl start fail2ban
  - systemctl reload ssh
  - mkdir -p /root/.vpsstore
  - |
    for i in $(seq 1 5); do
      sleep 30
      curl -X POST API_HOST/api/vps/INSTANCE_ID/credentials \
        -H "Authorization: Bearer API_TOKEN" \
        -H "Content-Type: application/json" \
        -d @/root/.vpsstore/credentials.json && break
    done',
  'VM.Standard.E4.Flex',
  1.0,
  6.0,
  50,
  CURRENT_TIMESTAMP
);
