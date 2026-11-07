#!/bin/sh
set -e
root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$root"
version="$(tr -d '[:space:]' < packaging/VERSION)"
out="$root/dist/linux-amd64"
pkg="$root/dist/packages"
mkdir -p "$out" "$pkg"

export CGO_ENABLED=0 GOOS=linux GOARCH=amd64
echo "building binaries $version linux/amd64"
go build -ldflags "-s -w" -o "$out/seccopy-client" ./cmd/client
go build -ldflags "-s -w" -o "$out/seccopy-server" ./cmd/server
go build -ldflags "-s -w" -o "$out/seccopy-publish" ./cmd/publish

nfpm_bin="$(command -v nfpm || true)"
if [ -z "$nfpm_bin" ]; then
	go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.4
	nfpm_bin="$(go env GOPATH)/bin/nfpm"
fi

for cfg in packaging/nfpm-client.yaml packaging/nfpm-server.yaml packaging/nfpm-publish.yaml packaging/nfpm-meta.yaml; do
	for packager in deb apk; do
		echo "nfpm $packager $cfg"
		"$nfpm_bin" pkg --config "$cfg" --packager "$packager" --target "$pkg"
	done
done

echo "packages in $pkg"
ls -l "$pkg"
