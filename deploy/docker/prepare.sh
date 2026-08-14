#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "${script_dir}/../.." && pwd)"
cd "${project_dir}"

umask 077

for command_name in docker openssl; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf '缺少命令：%s\n' "${command_name}" >&2
    exit 1
  fi
done
if ! docker compose version >/dev/null 2>&1; then
  printf '需要 Docker Compose v2（docker compose）。\n' >&2
  exit 1
fi

if [[ ! -f config.yaml ]]; then
  cp config.example.yaml config.yaml
  chmod 600 config.yaml
  printf '已从安全示例创建 config.yaml。\n'
fi

mkdir -p secrets
chmod 700 secrets
if [[ ! -s secrets/cookie_encryption_key ]]; then
  openssl rand -base64 32 > secrets/cookie_encryption_key
fi
if [[ ! -s secrets/session_secret ]]; then
  openssl rand -base64 48 > secrets/session_secret
fi

docker build --pull -t cyber-amber:local .

if [[ ! -s secrets/admin_password_hash ]]; then
  if [[ ! -t 0 ]]; then
    printf '首次初始化管理员密码必须在交互式终端中执行。\n' >&2
    exit 1
  fi
  read -r -s -p '管理员密码（至少 12 个字符）：' admin_password
  printf '\n'
  read -r -s -p '再次输入管理员密码：' admin_password_confirm
  printf '\n'
  if [[ "${admin_password}" != "${admin_password_confirm}" ]]; then
    unset admin_password admin_password_confirm
    printf '两次输入不一致。\n' >&2
    exit 1
  fi
  if (( ${#admin_password} < 12 )); then
    unset admin_password admin_password_confirm
    printf '管理员密码至少需要 12 个字符。\n' >&2
    exit 1
  fi
  printf '%s' "${admin_password}" | docker run --rm -i cyber-amber:local -hash-password=- > secrets/admin_password_hash
  unset admin_password admin_password_confirm
fi

chmod 600 config.yaml secrets/cookie_encryption_key secrets/session_secret secrets/admin_password_hash
docker compose config --quiet

printf '\n初始化完成。下一步：\n'
printf '  docker compose up -d\n'
printf '  ./deploy/docker/verify.sh\n'
printf '\n公开部署前请修改 config.yaml 的 app.base_url，并在反向代理启用 TLS。\n'
