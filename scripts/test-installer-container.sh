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
