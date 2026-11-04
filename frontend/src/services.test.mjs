import {test} from 'node:test';
import assert from 'node:assert/strict';
import {addService, hasService} from './services.js';

// The start of the Services window (internal/container ComposeTemplate).
const TEMPLATE = '# Services next to this session\'s dev container.\n#   dev:\n#     depends_on: [postgres]\nservices: {}\n';

test('addService turns "services: {}" into a section and adds the volume', () => {
    const out = addService(TEMPLATE, 'PostgreSQL');
    assert.ok(out.startsWith('# Services next'));
    assert.ok(!out.includes('services: {}'));
    assert.ok(out.includes('services:\n  # PostgreSQL (added by the Services window): postgres://dev:dev@postgres:5432/dev\n  postgres:\n    image: docker.io/library/postgres:17\n'));
    assert.ok(out.endsWith('      - pgdata:/var/lib/postgresql/data\nvolumes:\n  pgdata:\n'));
    assert.ok(hasService(out, 'postgres'));
    // The commented "dev:" in the header is not a service.
    assert.ok(!hasService(out, 'dev'));
});

test('addService adds to existing sections and keeps what follows', () => {
    const file = 'services:\n  dev:\n    environment: {A: b}\n\nvolumes:\n  old:\n\nx-note: kept\n';
    let out = addService(file, 'Redis');
    assert.ok(out.includes('    environment: {A: b}\n  # Redis'));
    assert.ok(out.includes('    image: docker.io/library/redis:7\n\nvolumes:\n  old:\n'));
    out = addService(out, 'MongoDB');
    assert.ok(out.includes('volumes:\n  old:\n  mongodata:\n\nx-note: kept\n'));
    assert.ok(out.indexOf('  mongo:') < out.indexOf('volumes:'));
    // Twice: unchanged.
    assert.equal(addService(out, 'MongoDB'), out);
});

test('addService on an empty file or one without services', () => {
    assert.equal(addService('', 'Redis'), 'services:\n  # Redis (added by the Services window): redis://redis:6379\n  redis:\n    image: docker.io/library/redis:7\n');
    const out = addService('volumes: {}\n', 'MySQL/MariaDB');
    assert.ok(out.startsWith('volumes:\n  mariadb:\nservices:\n') || out.includes('services:\n  # MySQL/MariaDB'));
    assert.ok(!out.includes('{}'));
});
