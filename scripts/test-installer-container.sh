#!/usr/bin/env bash
# Run the installer end to end inside a plain arm64 container image under
# qemu-user (needs docker with arm64 emulation registered; CI sets it up).
#
# What this proves: the stamped installer, the real linux/arm64 binary from a
# release bundle, the real useradd/chown, file layout, the health check and the
# uninstall work on a minimal Debian/Ubuntu userland. What it does not prove: a
# real systemd (there is none, a stub starts the binary), a Raspberry Pi OS or
# Ubuntu Server image, a reboot, or real hardware. Docs: docs/INSTALL.md.
#
# Since G6.3b the release is normally the components shape (the update helper and its
# units are in the bundle): the test checks the link layout, modes, owners, the real
# flock on /var/lib/oarfish-update and that the arm64 helper stub runs. When go and
# minisign are available (CI has both) the script also builds a legacy-shape release of
# the same commit with a throwaway key and the test installs it first, so that the main
# release is an upgrade of an alpha-style installation (the migration).
#
# Usage: scripts/test-installer-container.sh IMAGE RELEASE_DIR TEST_INSTALL_SH
#   IMAGE            e.g. debian:12 or ubuntu:24.04
#   RELEASE_DIR      release files signed with a throwaway key
#   TEST_INSTALL_SH  scripts/make-test-installer.sh output for that key and version
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
ROOT="$(repo_root)"
IMAGE="${1:?usage: $0 IMAGE RELEASE_DIR TEST_INSTALL_SH}"
REL="${2:?usage: $0 IMAGE RELEASE_DIR TEST_INSTALL_SH}"
INST="${3:?usage: $0 IMAGE RELEASE_DIR TEST_INSTALL_SH}"
command -v docker >/dev/null || die "docker not found"

STAGE="$(mktemp -d)"; trap 'rm -rf "$STAGE"' EXIT
mkdir "$STAGE/release"
cp "$REL"/* "$STAGE/release/"
cp "$INST" "$STAGE/install.sh"
cp "$ROOT/scripts/installer-container/run.sh" "$ROOT/scripts/installer-container/systemctl-stub.sh" "$STAGE/"
chmod +x "$STAGE/systemctl-stub.sh"

# Optional: a legacy-shape release of the same commit, signed with its own throwaway key.
if command -v go >/dev/null && command -v minisign >/dev/null && [ -f "$ROOT/backend/internal/web/dist/index.html" ]; then
  main_ver="$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$REL/manifest.json")"
  legacy_ver="$main_ver.legacy"
  LEGACY="$(mktemp -d)"; trap 'rm -rf "$STAGE" "$LEGACY"' EXIT
  OARFISH_BUNDLE_SHAPE=legacy VERSION="$legacy_ver" "$ROOT/scripts/release-bundle.sh" -o "$LEGACY/release" >/dev/null
  "$ROOT/scripts/make-checksums.sh" "$LEGACY/release" >/dev/null
  "$ROOT/scripts/sign-release.sh" --ephemeral "$LEGACY/throwaway.pub" "$LEGACY/release" >/dev/null
  "$ROOT/scripts/make-test-installer.sh" "$legacy_ver" "$LEGACY/throwaway.pub" "$STAGE/legacy-install.sh"
  mkdir "$STAGE/legacy-release"; cp "$LEGACY/release"/* "$STAGE/legacy-release/"
  echo "legacy-shape release $legacy_ver built for the migration step"
else
  echo "go, minisign or the frontend build is missing: the migration step is skipped"
fi

arch="$(docker run --rm --platform linux/arm64 "$IMAGE" uname -m)"
echo "container architecture: $arch"
[ "$arch" = aarch64 ] || die "the container is $arch, not aarch64; arm64 emulation is not working"

LOG="$(mktemp)"
set +e
docker run --rm --platform linux/arm64 -v "$STAGE:/work:ro" "$IMAGE" sh /work/run.sh 2>&1 | tee "$LOG"
status=${PIPESTATUS[0]}
set -e
{
  echo "### Installer in a plain arm64 container: \`$IMAGE\` (qemu-user)"
  echo '```'
  grep '^REPORT' "$LOG" | sed 's/^REPORT //'
  echo '```'
  if grep -q '^CONTAINER TEST PASSED' "$LOG"; then echo "Result: **passed**. No systemd in the container (a stub starts the binary); this is not Raspberry Pi OS or Ubuntu Server and not hardware."
  else echo "Result: **FAILED**"; fi
} | summary
rm -f "$LOG"
exit "$status"
