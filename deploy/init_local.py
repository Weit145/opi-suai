#!/usr/bin/env python3
"""Generate private local configuration without printing secret values."""
import os
from pathlib import Path
import secrets
import subprocess

root = Path(__file__).resolve().parent.parent
os.umask(0o077)
config = root / '.env.app'
if not config.exists():
    values = {key: secrets.token_hex(32) for key in ('POSTGRES_ADMIN_PASSWORD', 'APP_DB_PASSWORD', 'JWT_SECRET', 'INTEGRATION_SECRET')}
    values['DEMO_PASSWORD'] = secrets.token_urlsafe(18)
    config.write_text(''.join(f'{key}={value}\n' for key, value in values.items()))
config.chmod(0o600)
certs = root / 'deploy/certs'
certs.mkdir(exist_ok=True)
(root / '.local/backups').mkdir(parents=True, exist_ok=True)
if not (certs / 'server.crt').exists():
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '365', '-keyout', str(certs/'ca.key'), '-out', str(certs/'ca.crt'), '-subj', '/CN=OPI34 Local Lab CA', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.run(['openssl', 'req', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(certs/'server.key'), '-out', str(certs/'server.csr'), '-subj', '/CN=localhost'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    (certs/'extensions.cnf').write_text('subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n')
    subprocess.run(['openssl', 'x509', '-req', '-in', str(certs/'server.csr'), '-CA', str(certs/'ca.crt'), '-CAkey', str(certs/'ca.key'), '-CAcreateserial', '-out', str(certs/'server.crt'), '-days', '365', '-extfile', str(certs/'extensions.cnf')], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
for name in ('ca.crt','server.crt'):
    (certs/name).chmod(0o644)
print('Created .env.app and local TLS certificates. Secret values were not printed.')
print('HTTPS: https://localhost:8443. Demo account emails: customer/manager/warehouse/assembler/admin @lab.invalid.')
print('The common demonstration password is DEMO_PASSWORD in .env.app. This CA is not installed in the operating system.')
