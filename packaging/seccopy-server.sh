#!/bin/sh
set -e

if [ ! -f /etc/seccopy/server.env ]; then
	echo "seccopy-server: missing /etc/seccopy/server.env" >&2
	echo "copy /etc/seccopy/server.env.example and set SECCOPY_GPG_USER and SECCOPY_ROOT" >&2
	exit 1
fi

# shellcheck disable=SC1091
. /etc/seccopy/server.env

if [ -z "$SECCOPY_GPG_USER" ] || [ -z "$SECCOPY_ROOT" ]; then
	echo "seccopy-server: SECCOPY_GPG_USER and SECCOPY_ROOT are required in /etc/seccopy/server.env" >&2
	exit 1
fi

set -- \
	-gpg-user "$SECCOPY_GPG_USER" \
	-listen "${SECCOPY_LISTEN:-127.0.0.1:50051}" \
	-repo "${SECCOPY_REPO:-default}" \
	-max-reads "${SECCOPY_MAX_READS:-256}"

if [ -n "$SECCOPY_GPG" ]; then
	set -- "$@" -gpg "$SECCOPY_GPG"
fi
if [ -n "$SECCOPY_USER" ]; then
	set -- "$@" -user "$SECCOPY_USER"
fi
if [ -n "$SECCOPY_PASSWORD" ]; then
	set -- "$@" -password "$SECCOPY_PASSWORD"
fi

exec /usr/bin/seccopy-server "$@" "$SECCOPY_ROOT"
