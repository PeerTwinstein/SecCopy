#!/bin/sh
if [ -f /etc/seccopy/server.env ]; then
	exit 0
fi
echo "seccopy-server: copy /etc/seccopy/server.env.example to /etc/seccopy/server.env and set SECCOPY_GPG_USER and SECCOPY_ROOT" >&2
exit 0
