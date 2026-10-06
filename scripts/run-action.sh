#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'IFTTT: %s\n' "$1" >&2; exit 3; }

[[ "${GITHUB_EVENT_NAME:-}" == pull_request ]] || fail 'This Action requires a pull_request event.'
case "${IFTTT_FAIL_ON_FINDINGS:-false}" in
  true|false) ;;
  *) fail 'fail-on-findings must be true or false.' ;;
esac

action_path="${IFTTT_ACTION_PATH:?Missing Action path}"
version="$(tr -d '\r\n' < "$action_path/action-version")"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'Invalid embedded release version.'
repository="${IFTTT_ACTION_REPOSITORY:-${GITHUB_REPOSITORY:-}}"
[[ "$repository" =~ ^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$ ]] || fail 'Missing or invalid Action repository.'

case "${RUNNER_OS:-}/${RUNNER_ARCH:-}" in
  Linux/X64) platform=linux_amd64 ;;
  Linux/ARM64) platform=linux_arm64 ;;
  macOS/ARM64) platform=darwin_arm64 ;;
  Windows/X64) platform=windows_amd64 ;;
  *) fail 'Supported runners: Linux x64/arm64, macOS arm64, Windows x64.' ;;
esac

asset="ifttt_${platform}.tar.gz"
download_dir="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ifttt.XXXXXXXX")"
trap 'rm -rf "$download_dir"' EXIT
url="${GITHUB_SERVER_URL:-https://github.com}/${repository}/releases/download/${version}"
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 120 \
  --proto '=https' --proto-redir '=https' "$url/$asset" -o "$download_dir/$asset" || fail 'Could not download release binary.'
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 120 \
  --proto '=https' --proto-redir '=https' "$url/checksums.txt" -o "$download_dir/checksums.txt" || fail 'Could not download release checksums.'

expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$download_dir/checksums.txt")"
[[ "$expected" =~ ^[0-9a-f]{64}$ ]] || fail 'Release checksum is missing or invalid.'
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$download_dir/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$download_dir/$asset" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || fail 'Release checksum verification failed.'

binary=ifttt
[[ "$platform" != windows_amd64 ]] || binary=ifttt.exe
tar -xzf "$download_dir/$asset" -C "$download_dir" "$binary" || fail 'Could not extract release binary.'
chmod +x "$download_dir/$binary" || fail 'Could not make release binary executable.'
args=(check --github "--fail-on-findings=${IFTTT_FAIL_ON_FINDINGS:-false}")
if [[ -n "${IFTTT_BASE_REF:-}" ]]; then args+=("--base=$IFTTT_BASE_REF"); fi
"$download_dir/$binary" "${args[@]}"
