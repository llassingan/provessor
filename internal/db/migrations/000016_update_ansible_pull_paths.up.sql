UPDATE templates
SET cloud_init_yaml = replace(
    replace(
        cloud_init_yaml,
        'https://github.com/your-org/vps-store-ansible.git',
        'https://github.com/llassingan/provessor.git'
    ),
    'playbook_names: [docker.yml]',
    'playbook_names: [ansible/templates/docker/playbook.yml]'
)
WHERE name = 'Docker'
  AND type = 'predefined';

UPDATE templates
SET cloud_init_yaml = replace(
    replace(
        cloud_init_yaml,
        'https://github.com/your-org/vps-store-ansible.git',
        'https://github.com/llassingan/provessor.git'
    ),
    'playbook_names: [nodejs.yml]',
    'playbook_names: [ansible/templates/nodejs/playbook.yml]'
)
WHERE name = 'Node.js'
  AND type = 'predefined';

UPDATE templates
SET cloud_init_yaml = replace(
    replace(
        cloud_init_yaml,
        'https://github.com/your-org/vps-store-ansible.git',
        'https://github.com/llassingan/provessor.git'
    ),
    'playbook_names: [wordpress.yml]',
    'playbook_names: [ansible/templates/wordpress/playbook.yml]'
)
WHERE name = 'WordPress'
  AND type = 'predefined';

UPDATE templates
SET cloud_init_yaml = replace(
    replace(
        cloud_init_yaml,
        'https://github.com/your-org/vps-store-ansible.git',
        'https://github.com/llassingan/provessor.git'
    ),
    'playbook_names: [ubuntu.yml]',
    'playbook_names: [ansible/templates/ubuntu/playbook.yml]'
)
WHERE name = 'Ubuntu'
  AND type = 'predefined';
