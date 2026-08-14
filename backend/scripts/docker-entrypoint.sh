#!/bin/sh
set -eu

SERVICE="${AIBOT_SERVICE:-}"
CONFIG_PATH="${AIBOT_CONFIG_PATH:-/app/config/config.yaml}"
HOSTNAME_VALUE="${HOSTNAME:-}"

if [ -z "$SERVICE" ]; then
	echo "AIBOT_SERVICE is required" >&2
	exit 1
fi

case "$SERVICE" in
	api|ws|llm|push|gateway|pay)
		;;
	*)
		echo "unsupported AIBOT_SERVICE: $SERVICE" >&2
		exit 1
		;;
esac

pod_ordinal() {
	if [ -z "$HOSTNAME_VALUE" ]; then
		return 1
	fi

	ordinal="${HOSTNAME_VALUE##*-}"
	case "$ordinal" in
		""|*[!0-9]*)
			return 1
			;;
	esac

	printf "%s" "$ordinal"
}

ensure_machine_id() {
	case "$SERVICE" in
		api|ws|llm)
			;;
		*)
			return
			;;
	esac

	if [ -n "${AIBOT_SNOWFLAKE_MACHINE_ID:-}" ]; then
		return
	fi

	ordinal="$(pod_ordinal || true)"
	if [ -z "$ordinal" ]; then
		return
	fi

	base="${AIBOT_MACHINE_ID_BASE:-0}"
	case "$base" in
		""|*[!0-9]*)
			echo "AIBOT_MACHINE_ID_BASE must be a non-negative integer" >&2
			exit 1
			;;
	esac

	machine_id=$((base + ordinal))
	if [ "$machine_id" -gt 1023 ]; then
		echo "computed machine id exceeds snowflake limit: $machine_id" >&2
		exit 1
	fi

	export AIBOT_SNOWFLAKE_MACHINE_ID="$machine_id"
}

ensure_ws_node_id() {
	if [ "$SERVICE" != "ws" ] || [ -n "${AIBOT_SERVER_NODE_ID:-}" ]; then
		return
	fi

	if [ -z "$HOSTNAME_VALUE" ]; then
		echo "AIBOT_SERVER_NODE_ID is required for ws when HOSTNAME is unavailable" >&2
		exit 1
	fi

	export AIBOT_SERVER_NODE_ID="$HOSTNAME_VALUE"
}

ensure_machine_id
ensure_ws_node_id

exec "/app/bin/$SERVICE" "$CONFIG_PATH"
