#!/usr/bin/env bash
# Sync CREATORR_PUBLIC_BASE_URL in project .env to this machine's LAN IPv4 + published port.
# Compose loads .env for ${…} substitution (see docker-compose.yml / override).
# Safe to run before every `docker compose up`. Skips when .env has a non-auto value.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${CREATORR_ENV_FILE:-$ROOT/.env}"
PORT="${CREATORR_PORT:-8787}"
MARKER="# creatorr-dev-auto-public-base-url"

is_rfc1918() {
	local a b
	IFS=. read -r a b _ _ <<<"$1" || return 1
	[[ "$a" == "10" ]] && return 0
	[[ "$a" == "192" && "$b" == "168" ]] && return 0
	[[ "$a" == "172" && "$b" -ge 16 && "$b" -le 31 ]] && return 0
	return 1
}

# Docker / VM bridges are usually not reachable as "the host" from LAN clients.
is_bridge_iface() {
	case "$1" in
	docker* | br-* | br0 | virbr* | podman* | cni* | flannel* | veth*) return 0 ;;
	*) return 1 ;;
	esac
}

detect_lan_ipv4() {
	local ip iface

	if command -v ip >/dev/null 2>&1; then
		ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit }}' || true)"
		if [[ -n "$ip" ]] && is_rfc1918 "$ip"; then
			# Reject route-src when it only exists on a bridge (sandbox / odd routing).
			iface="$(ip -4 -o addr show to "$ip" 2>/dev/null | awk '{print $2; exit}' || true)"
			if [[ -n "$iface" ]] && ! is_bridge_iface "$iface"; then
				printf '%s\n' "$ip"
				return 0
			fi
		fi

		while read -r iface ip; do
			[[ -z "$iface" || -z "$ip" ]] && continue
			is_bridge_iface "$iface" && continue
			is_rfc1918 "$ip" || continue
			printf '%s\n' "$ip"
			return 0
		done < <(ip -4 -o addr show scope global 2>/dev/null | awk '{gsub(/\/.*/, "", $4); print $2, $4}')
	fi

	if command -v hostname >/dev/null 2>&1; then
		for ip in $(hostname -I 2>/dev/null || true); do
			is_rfc1918 "$ip" || continue
			printf '%s\n' "$ip"
			return 0
		done
	fi

	echo "could not detect a LAN IPv4 (set CREATORR_PUBLIC_BASE_URL manually)" >&2
	return 1
}

url_for() {
	printf 'http://%s:%s\n' "$1" "$PORT"
}

write_auto_url() {
	local url="$1"
	local tmp
	tmp="$(mktemp)"
	if [[ -f "$ENV_FILE" ]]; then
		grep -v -e "^${MARKER}$" -e '^CREATORR_PUBLIC_BASE_URL=' "$ENV_FILE" >"$tmp" || true
	fi
	{
		cat "$tmp"
		# Ensure trailing newline before marker when file had content without one.
		printf '%s\nCREATORR_PUBLIC_BASE_URL=%s\n' "$MARKER" "$url"
	} >"${tmp}.out"
	mv "${tmp}.out" "$ENV_FILE"
	rm -f "$tmp"
}

main() {
	local ip url existing
	ip="$(detect_lan_ipv4)"
	url="$(url_for "$ip")"

	if [[ -f "$ENV_FILE" ]] && grep -q '^CREATORR_PUBLIC_BASE_URL=' "$ENV_FILE"; then
		existing="$(grep '^CREATORR_PUBLIC_BASE_URL=' "$ENV_FILE" | tail -n1 | cut -d= -f2-)"
		if ! grep -q "^${MARKER}$" "$ENV_FILE" 2>/dev/null; then
			printf '%s\n' "$existing"
			return 0
		fi
		write_auto_url "$url"
		printf '%s\n' "$url"
		return 0
	fi

	write_auto_url "$url"
	printf '%s\n' "$url"
}

main "$@"
