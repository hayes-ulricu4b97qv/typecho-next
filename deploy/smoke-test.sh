#!/usr/bin/env bash
set -euo pipefail

image="${1:-typecho-next:ci}"
run_id="$(date -u +%Y%m%dT%H%M%S)-$$-${RANDOM}"
app_volume="typecho-deploy-smoke-${run_id}"
restore_volume="typecho-deploy-restore-${run_id}"
app_first="typecho-deploy-first-${run_id}"
app_second="typecho-deploy-second-${run_id}"
app_restored="typecho-deploy-restored-${run_id}"
smoke_dir="$(mktemp -d)"
base="http://127.0.0.1:8081"
gif_base64='R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'
python_cmd=python3
if [[ -n "${MSYSTEM:-}" ]]; then
  python_cmd=python.exe
fi

docker_cmd() {
  if [[ -n "${MSYSTEM:-}" ]]; then
    MSYS_NO_PATHCONV=1 docker "$@"
  else
    command docker "$@"
  fi
}

host_path() {
  if [[ -n "${MSYSTEM:-}" ]]; then
    cygpath -aw "$1"
  else
    printf '%s\n' "$1"
  fi
}

cleanup() {
  for container in "$app_first" "$app_second" "$app_restored"; do
    docker_cmd rm -f "$container" >/dev/null 2>&1 || true
  done
  for volume in "$app_volume" "$restore_volume"; do
    docker_cmd volume rm "$volume" >/dev/null 2>&1 || true
  done
  rm -rf -- "$smoke_dir"
}
trap cleanup EXIT

wait_healthy() {
  local name="$1"
  local state health
  for _ in $(seq 1 45); do
    state="$(docker_cmd inspect --format '{{.State.Status}}' "$name")"
    health="$(docker_cmd inspect --format '{{.State.Health.Status}}' "$name")"
    if [[ "$health" == healthy ]]; then
      return 0
    fi
    if [[ "$state" != running ]]; then
      docker_cmd logs "$name" >&2 || true
      return 1
    fi
    sleep 1
  done
  docker_cmd logs "$name" >&2 || true
  printf 'container did not become healthy: %s\n' "$name" >&2
  return 1
}

http_status() {
  local name="$1" path="$2"
  shift 2
  docker_cmd exec "$name" curl --silent --show-error --output /dev/null --write-out '%{http_code}' "$@" "$base$path"
}

expect_status() {
  local name="$1" path="$2" expected="$3" actual
  shift 3
  actual="$(http_status "$name" "$path" "$@")"
  if [[ "$actual" != "$expected" ]]; then
    printf '%s %s returned %s, expected %s\n' "$name" "$path" "$actual" "$expected" >&2
    return 1
  fi
}

start_app() {
  local name="$1" volume="$2"
  docker_cmd run -d --name "$name" --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges:true --user 10001:10001 \
    --mount "type=volume,source=$volume,target=/var/lib/typecho" \
    --env APP_LISTEN_HOST=0.0.0.0 --env APP_PORT=8081 \
    --env APP_DATA_DIR=/var/lib/typecho --env TMPDIR=/var/lib/typecho \
    --env APP_WEB_DIR=/app/web/dist --env APP_COOKIE_SECURE=false \
    "$image" >/dev/null
}

docker_cmd image inspect "$image" >/dev/null
docker_cmd volume create "$app_volume" >/dev/null
start_app "$app_first" "$app_volume"
wait_healthy "$app_first"
expect_status "$app_first" /healthz 200
expect_status "$app_first" /readyz 503
[[ "$(docker_cmd exec "$app_first" id -u)" == 10001 ]]
[[ "$(docker_cmd exec "$app_first" id -g)" == 10001 ]]

setup_json='{"siteName":"Docker smoke","siteUrl":"https://smoke.invalid","adminUsername":"owner","adminPassword":"smoke-password-123","adminEmail":"owner@example.invalid","timezone":"UTC"}'
setup_headers="$(docker_cmd exec "$app_first" curl --silent --show-error --dump-header - --output /dev/null \
  --cookie-jar /var/lib/typecho/cookies --header 'Content-Type: application/json' \
  --data-binary "$setup_json" "$base/api/v1/setup")"
grep -Eq '^HTTP/[0-9.]+ 201([[:space:]]|$)' <<<"$setup_headers"
csrf="$(sed -nE 's/^Set-Cookie:[[:space:]]*app_csrf=([^;]+).*/\1/ip' <<<"$setup_headers" | head -n 1)"
[[ -n "$csrf" ]]
expect_status "$app_first" /readyz 200

docker_cmd exec "$app_first" sh -ec 'printf "%s" "$1" | base64 -d > /var/lib/typecho/smoke.gif' sh "$gif_base64"
upload_status="$(docker_cmd exec "$app_first" curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --cookie /var/lib/typecho/cookies --header "X-CSRF-Token: $csrf" \
  --form 'file=@/var/lib/typecho/smoke.gif;type=image/gif;filename=smoke.gif' "$base/api/v1/media")"
[[ "$upload_status" == 201 ]]

media_json="$(docker_cmd exec "$app_first" curl --fail --silent --show-error --cookie /var/lib/typecho/cookies "$base/api/v1/media")"
asset_id="$("$python_cmd" -c 'import json,sys; print(json.load(sys.stdin)["items"][0]["id"])' <<<"$media_json")"
asset_url="$("$python_cmd" -c 'import json,sys; print(json.load(sys.stdin)["items"][0]["url"])' <<<"$media_json")"
post_json="$(MSYS_NO_PATHCONV=1 "$python_cmd" -c 'import json,sys; print(json.dumps({"title":"Docker volume smoke","slug":"docker-volume-smoke","content":f"![smoke]({sys.argv[1]})","status":"published"}))' "$asset_url")"
post_status="$(docker_cmd exec "$app_first" curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --cookie /var/lib/typecho/cookies --header "X-CSRF-Token: $csrf" \
  --header 'Content-Type: application/json' --data-binary "$post_json" "$base/api/v1/content/posts")"
[[ "$post_status" == 201 ]]

docker_cmd stop "$app_first" >/dev/null
docker_cmd rm "$app_first" >/dev/null
start_app "$app_second" "$app_volume"
wait_healthy "$app_second"
expect_status "$app_second" /readyz 200
expect_status "$app_second" /api/v1/auth/me 200 --cookie /var/lib/typecho/cookies
media_after_restart="$(docker_cmd exec "$app_second" curl --fail --silent --show-error --cookie /var/lib/typecho/cookies "$base/api/v1/media")"
"$python_cmd" -c 'import json,sys; expected=sys.argv[1]; actual=json.load(sys.stdin)["items"][0]["id"]; assert actual == expected, actual' "$asset_id" <<<"$media_after_restart"
post_after_restart="$(docker_cmd exec "$app_second" curl --fail --silent --show-error "$base/api/v1/content/posts/docker-volume-smoke")"
MSYS_NO_PATHCONV=1 "$python_cmd" -c 'import json,sys; expected=sys.argv[1]; actual=json.load(sys.stdin)["content"]; assert actual == expected, actual' "![smoke]($asset_url)" <<<"$post_after_restart"
docker_cmd exec "$app_second" curl --fail --silent --show-error --output /var/lib/typecho/media-check.gif "$base$asset_url"
[[ "$(docker_cmd exec "$app_second" sh -ec 'base64 /var/lib/typecho/media-check.gif | tr -d "\n"')" == "$gif_base64" ]]

docker_cmd exec "$app_second" curl --fail --silent --show-error --cookie /var/lib/typecho/cookies \
  --output /var/lib/typecho/backup.zip "$base/api/v1/backup"
docker_cmd cp "$app_second:/var/lib/typecho/backup.zip" "$(host_path "$smoke_dir/backup.zip")" >/dev/null
"$python_cmd" -m zipfile -t "$smoke_dir/backup.zip" >/dev/null
mkdir "$smoke_dir/restore-source"
"$python_cmd" -m zipfile -e "$smoke_dir/backup.zip" "$smoke_dir/restore-source"
[[ -s "$smoke_dir/restore-source/app.db" && -d "$smoke_dir/restore-source/media" ]]
docker_cmd volume create "$restore_volume" >/dev/null
docker_cmd run --rm --network none --user 0:0 --cap-drop ALL --cap-add CHOWN \
  --mount "type=volume,source=$restore_volume,target=/restore" \
  --mount "type=bind,source=$(host_path "$smoke_dir/restore-source"),target=/source,readonly" \
  --entrypoint /bin/sh "$image" -ec \
  'cp /source/app.db /restore/app.db; mkdir -p /restore/uploads; cp -a /source/media/. /restore/uploads/; chown -R 10001:10001 /restore' >/dev/null

start_app "$app_restored" "$restore_volume"
wait_healthy "$app_restored"
expect_status "$app_restored" /readyz 200
login_status="$(docker_cmd exec "$app_restored" curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --cookie-jar /var/lib/typecho/cookies --header 'Content-Type: application/json' \
  --data-binary '{"username":"owner","password":"smoke-password-123"}' "$base/api/v1/auth/login")"
[[ "$login_status" == 200 ]]
expect_status "$app_restored" /api/v1/auth/me 200 --cookie /var/lib/typecho/cookies
media_after_restore="$(docker_cmd exec "$app_restored" curl --fail --silent --show-error --cookie /var/lib/typecho/cookies "$base/api/v1/media")"
"$python_cmd" -c 'import json,sys; expected=sys.argv[1]; actual=json.load(sys.stdin)["items"][0]["id"]; assert actual == expected, actual' "$asset_id" <<<"$media_after_restore"
restored_post="$(docker_cmd exec "$app_restored" curl --fail --silent --show-error "$base/api/v1/content/posts/docker-volume-smoke")"
MSYS_NO_PATHCONV=1 "$python_cmd" -c 'import json,sys; expected=sys.argv[1]; actual=json.load(sys.stdin)["content"]; assert actual == expected, actual' "![smoke]($asset_url)" <<<"$restored_post"
docker_cmd exec "$app_restored" curl --fail --silent --show-error --output /var/lib/typecho/media-restored.gif "$base$asset_url"
[[ "$(docker_cmd exec "$app_restored" sh -ec 'base64 /var/lib/typecho/media-restored.gif | tr -d "\n"')" == "$gif_base64" ]]

printf 'Docker smoke passed: isolated setup, media, persistent restart, backup and restore.\n'
