#!/bin/sh
set -eu
task_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$task_root"
task_stage=$(mktemp -d "${TMPDIR:-/tmp}/sharkcool-release.XXXXXX")
trap 'rm -rf "$task_stage"' 0
mkdir -p dist
make desktop
for task_arch in amd64 arm64; do
  task_name="sharkcool-linux-$task_arch"
  task_out="$task_stage/$task_name"
  mkdir -p "$task_out/packaging" "$task_out/scripts" "$task_out/packaging/licenses" "$task_out/examples"
  CGO_ENABLED=0 GOOS=linux GOARCH="$task_arch" go build -buildvcs=false -trimpath -ldflags='-s -w' -o "$task_out/sharkcool" ./cmd/sharkcool
  if [ "$task_arch" = amd64 ]; then
    install -m755 dist/sharkcool-window "$task_out/sharkcool-window"
  fi
  cp README.md LICENSE PROTOCOL.md "$task_out/"
  cp packaging/70-sharkcool.rules packaging/sharkcool.desktop packaging/sharkcool.svg packaging/ASSET-NOTICE.md "$task_out/packaging/"
  install -m 644 packaging/licenses/godbus-dbus.txt "$task_out/packaging/licenses/godbus-dbus.txt"
  cp scripts/install.sh "$task_out/scripts/"
  cp examples/scenes.json "$task_out/examples/"
  chmod +x "$task_out/scripts/install.sh"
  tar -C "$task_stage" -czf "dist/$task_name.tar.gz" "$task_name"
done
task_source="$task_stage/sharkcool-source"
mkdir -p "$task_source"
cp -R brb02 cmd internal examples vendor packaging scripts desktop "$task_source/"
rm -rf "$task_source/scripts/__pycache__" "$task_source/desktop/sensors/bin" "$task_source/desktop/sensors/obj"
cp go.mod go.sum Makefile README.md PROTOCOL.md LICENSE .gitignore "$task_source/"
if [ -d .github ]; then cp -R .github "$task_source/"; fi
if [ -d docs ]; then cp -R docs "$task_source/"; fi
tar -C "$task_stage" -czf dist/sharkcool-source.tar.gz sharkcool-source
(cd dist && sha256sum sharkcool-linux-amd64.tar.gz sharkcool-linux-arm64.tar.gz sharkcool-source.tar.gz) > dist/SHA256SUMS
