#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd -- "${script_dir}/../.." && pwd)"
cd "${project_dir}"

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  printf '需要 Docker Engine 与 Docker Compose v2。\n' >&2
  exit 1
fi

container_id="$(docker compose ps -q cyber-amber)"
if [[ -z "${container_id}" ]]; then
  printf 'cyber-amber 容器尚未启动。\n' >&2
  exit 1
fi

for attempt in {1..30}; do
  health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${container_id}")"
  if [[ "${health}" == "healthy" ]]; then
    break
  fi
  if [[ "${health}" == "unhealthy" ]]; then
    docker compose logs --tail=100 cyber-amber >&2
    printf '容器健康检查失败。\n' >&2
    exit 1
  fi
  if (( attempt == 30 )); then
    docker compose logs --tail=100 cyber-amber >&2
    printf '等待容器健康检查超时。\n' >&2
    exit 1
  fi
  sleep 2
done

docker compose exec -T cyber-amber cyber-amber -config /config/config.yaml -healthcheck
printf '容器内 healthcheck：通过\n'

if command -v curl >/dev/null 2>&1; then
  curl --fail --silent --show-error http://127.0.0.1:8080/healthz >/dev/null
  curl --fail --silent --show-error http://127.0.0.1:8080/readyz >/dev/null
  printf 'HTTP healthz/readyz：通过\n'
else
  printf '未安装 curl，已跳过宿主 HTTP 检查。\n'
fi

docker compose ps
