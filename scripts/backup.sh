#!/usr/bin/env bash
# Backup and restore the SQLite volume used by the Docker stack.
#
# Backup:   ./scripts/backup.sh backup            (KEEP=n keeps n snapshots)
# Restore:  ./scripts/backup.sh restore FILE=...
#
# Backups use the SQLite online backup API (.backup), so snapshots are
# consistent even while the API is running (WAL-safe, no downtime).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUP_DIR="${BACKUP_DIR:-$ROOT/backups}"
KEEP="${KEEP:-14}"
VOLUME="${ORACLE_DATA_VOLUME:-oracle-data}"
API_UID="${API_UID:-1000}"

backup() {
	mkdir -p "$BACKUP_DIR"
	local name="oracle-$(date +%Y%m%d-%H%M%S).db"

	docker run --rm \
		-v "$VOLUME:/data" \
		-v "$BACKUP_DIR:/backup" \
		alpine sh -c "apk add --no-cache sqlite >/dev/null && sqlite3 /data/oracle.db \".backup /backup/$name\""

	echo "backed up to $BACKUP_DIR/$name"

	local count
	count="$(find "$BACKUP_DIR" -maxdepth 1 -name 'oracle-*.db' | wc -l | tr -d ' ')"
	if [ "$count" -gt "$KEEP" ]; then
		find "$BACKUP_DIR" -maxdepth 1 -name 'oracle-*.db' -printf '%T@ %p\n' |
			sort -nr | tail -n +$((KEEP + 1)) | cut -d' ' -f2- | xargs rm
		echo "pruned old snapshots (keeping $KEEP)"
	fi
}

restore() {
	local file="${1:-${FILE:-}}"
	if [ -z "$file" ]; then
		echo "usage: $0 restore <backup-file>" >&2
		exit 1
	fi
	[ -f "$file" ] || { echo "no such backup: $file" >&2; exit 1; }

	(cd "$ROOT" && docker compose stop api)
	trap '(cd "$ROOT" && docker compose start api)' EXIT

	# Stale -wal/-shm files belong to the old database and would corrupt the
	# restored one, so they are removed along with the main file.
	local src_dir
	src_dir="$(cd "$(dirname "$file")" && pwd)"
	docker run --rm \
		-v "$VOLUME:/data" \
		-v "$src_dir:/backup" \
		alpine sh -c \
		"rm -f /data/oracle.db /data/oracle.db-wal /data/oracle.db-shm && cp /backup/$(basename "$file") /data/oracle.db && chown $API_UID /data/oracle.db"

	echo "restored $(basename "$file"); starting api"
}

case "${1:-}" in
backup) backup ;;
restore) restore "${2:-}" ;;
*) echo "usage: $0 {backup|restore <file>}" >&2 && exit 1 ;;
esac
