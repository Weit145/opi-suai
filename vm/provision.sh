#!/usr/bin/env bash
# Runs as root INSIDE a new Ubuntu 24.04 VM, never on the host.
set -Eeuo pipefail
exec > >(tee -a /var/log/opi-provision.log) 2>&1
trap 'echo "Provisioning failed at line $LINENO"' ERR
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

if [[ -f /var/lib/opi-redmine-ready ]]; then
    echo 'Redmine already provisioned.'
    exit 0
fi

echo '=== Installing Ruby, PostgreSQL, nginx, Git and build dependencies ==='
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl git openssh-server \
    ruby ruby-dev ruby-bundler build-essential pkg-config libpq-dev libyaml-dev \
    zlib1g-dev libssl-dev libxml2-dev libxslt1-dev libffi-dev postgresql nginx

# A small swap file prevents native gem compilation from exhausting guest memory.
if [[ ! -f /swapfile ]]; then
    fallocate -l 1G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    printf '/swapfile none swap sw 0 0\n' >> /etc/fstab
fi

systemctl enable --now postgresql
id redmine >/dev/null 2>&1 || useradd --system --create-home --home-dir /opt/redmine --shell /bin/bash redmine
echo '=== Downloading and verifying Redmine 6.1.5 ==='
curl -fL --retry 3 https://www.redmine.org/releases/redmine-6.1.5.tar.gz -o /root/redmine-6.1.5.tar.gz
printf '%s  %s\n' '4b653d8863c012fdb226aedf6f8a18bdfa08d04625d15e8aafbbc3c4f6b70144' \
    /root/redmine-6.1.5.tar.gz | sha256sum -c -
tar -xzf /root/redmine-6.1.5.tar.gz --strip-components=1 -C /opt/redmine
tar -xzf /root/bleuclair.tar.gz -C /opt/redmine/themes
# Redmine lists Puma only in its test group; make the same gem available in production.
sed -i "/^  gem 'puma'$/d" /opt/redmine/Gemfile
printf "gem 'puma'\n" > /opt/redmine/Gemfile.local

echo '=== Creating a separate PostgreSQL role and database ==='
DB_PASSWORD=$(python3 -c 'import json; print(json.load(open("/root/redmine-credentials.json"))["db_password"])')
if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='redmine'" | grep -q 1; then
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "CREATE ROLE redmine LOGIN PASSWORD '$DB_PASSWORD';"
fi
if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='redmine'" | grep -q 1; then
    runuser -u postgres -- createdb --owner=redmine --encoding=UTF8 redmine
fi
umask 077
printf 'production:\n  adapter: postgresql\n  host: 127.0.0.1\n  database: redmine\n  username: redmine\n  password: "%s"\n  encoding: utf8\n  pool: 5\n' \
    "$DB_PASSWORD" > /opt/redmine/config/database.yml
python3 - <<'PY'
import json
from pathlib import Path
settings = json.loads(Path('/root/redmine-credentials.json').read_text())
Path('/opt/redmine/.env.production').write_text('RAILS_ENV=production\nRAILS_SERVE_STATIC_FILES=true\nSECRET_KEY_BASE=' + settings['secret_key_base'] + '\n')
Path('/opt/redmine/config/initializers/secret_token.rb').write_text('Rails.application.config.secret_key_base = ENV.fetch("SECRET_KEY_BASE")\n')
Path('/opt/redmine/admin-password').write_text(settings['admin_password'])
PY
umask 022
chown -R redmine:redmine /opt/redmine
chmod 600 /opt/redmine/config/database.yml /opt/redmine/.env.production /opt/redmine/admin-password

echo '=== Installing gems and initializing the database ==='
gem install bundler --version 2.5.23 --no-document
cd /opt/redmine
runuser -u redmine -- bash <<'RUBY_SETUP'
set -Eeuo pipefail
cd /opt/redmine
set -a
source .env.production
set +a
bundle _2.5.23_ config set --local path vendor/bundle
bundle _2.5.23_ config set --local without 'development test'
bundle _2.5.23_ install --jobs 1 --retry 3
bundle _2.5.23_ exec rake db:migrate
REDMINE_LANG=ru bundle _2.5.23_ exec rake redmine:load_default_data
bundle _2.5.23_ exec rails runner -e production '
  admin = User.find_by!(login: "admin")
  admin.password = File.read("admin-password").strip
  admin.password_confirmation = admin.password
  admin.must_change_passwd = false
  admin.language = "ru"
  admin.save!
  Setting.ui_theme = "bleuclair"
  Setting.default_language = "ru"
  Setting.app_title = "Redmine — ОПИ"
  Setting.host_name = "localhost:8082"
  abort "Bleuclair is missing" unless Redmine::Themes.theme("bleuclair")
  puts "Redmine initialized with Bleuclair"
'
bundle _2.5.23_ exec rake assets:precompile
RUBY_SETUP

echo '=== Configuring Puma and systemd ==='
cat > /opt/redmine/config/puma-vm.rb <<'PUMA'
environment 'production'
directory '/opt/redmine'
bind 'tcp://127.0.0.1:3000'
threads 1, 3
workers 0
PUMA
chown redmine:redmine /opt/redmine/config/puma-vm.rb
cat > /etc/systemd/system/redmine.service <<'SERVICE'
[Unit]
Description=Redmine 6.1.5 (Puma)
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=redmine
Group=redmine
WorkingDirectory=/opt/redmine
EnvironmentFile=/opt/redmine/.env.production
ExecStart=/usr/local/bin/bundle _2.5.23_ exec puma -C config/puma-vm.rb
Restart=on-failure
RestartSec=5
TimeoutStopSec=45
UMask=0027
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SERVICE

echo '=== Configuring nginx ==='
cat > /etc/nginx/sites-available/redmine <<'NGINX'
server {
    listen 80 default_server;
    server_name _;
    client_max_body_size 20m;
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 120s;
    }
}
NGINX
if [[ -L /etc/nginx/sites-enabled/default ]]; then
    unlink /etc/nginx/sites-enabled/default
fi
ln -sfn /etc/nginx/sites-available/redmine /etc/nginx/sites-enabled/redmine
nginx -t
systemctl daemon-reload
systemctl enable --now redmine nginx
systemctl reload nginx
runuser -u student -- git config --global user.name 'weit'
runuser -u student -- git config --global user.email 'weit@opi-vm.local'
runuser -u student -- git config --global init.defaultBranch main

for attempt in $(seq 1 60); do
    if curl -fsS http://127.0.0.1/ | grep -q 'theme-Bleuclair'; then
        touch /var/lib/opi-redmine-ready
        echo '=== READY: Redmine + Bleuclair + PostgreSQL + Git ==='
        exit 0
    fi
    sleep 2
done
echo 'Redmine did not become ready. Check journalctl -u redmine.'
exit 1
