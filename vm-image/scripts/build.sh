#!/usr/bin/env bash
# vm-image/scripts/build.sh — orchestrate a Linux build of the Gemba
# microVM kernel + rootfs and emit dist/vm/{vmlinux,rootfs.ext4,manifest.json}.
#
# Invoked by `make vm-image` on a Linux host. Refuses to run anywhere
# else (the kernel + Buildroot toolchain are Linux-only).
#
# Environment knobs:
#   BUILDROOT_DIR   path to a Buildroot 2024.x checkout (required)
#   KERNEL_SRC_DIR  path to an unpacked linux-6.1 source tree (required)
#   GEMBA_RELEASE_KEY  optional path to an ed25519 private key (PEM); if
#                      set, emits manifest.sig alongside manifest.json
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "vm-image: must be built on Linux (got $(uname -s))" >&2
  exit 1
fi

: "${BUILDROOT_DIR:?BUILDROOT_DIR is required (path to a Buildroot 2024.x checkout)}"
: "${KERNEL_SRC_DIR:?KERNEL_SRC_DIR is required (path to an unpacked linux-6.1 source tree)}"

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
vm_image_dir="$repo_root/vm-image"
dist_dir="$repo_root/dist/vm"
mkdir -p "$dist_dir"

# --- 1. Build the kernel ---
echo ">> building kernel from $KERNEL_SRC_DIR"
cp "$vm_image_dir/kernel/gemba_kernel.config" "$KERNEL_SRC_DIR/.config"
( cd "$KERNEL_SRC_DIR" && make olddefconfig && make -j"$(nproc)" vmlinux )
cp "$KERNEL_SRC_DIR/vmlinux" "$dist_dir/vmlinux"
kernel_version="$(cd "$KERNEL_SRC_DIR" && make -s kernelversion)"

# --- 2. Build the rootfs ---
echo ">> building rootfs via Buildroot at $BUILDROOT_DIR"
br_out="$vm_image_dir/buildroot/output"
mkdir -p "$br_out"
make -C "$BUILDROOT_DIR" \
  BR2_EXTERNAL="$vm_image_dir/buildroot/external" \
  O="$br_out" \
  defconfig BR2_DEFCONFIG="$vm_image_dir/buildroot/gemba_defconfig"
make -C "$br_out" -j"$(nproc)"
cp "$br_out/images/rootfs.ext4" "$dist_dir/rootfs.ext4"

# --- 3. Write manifest.json ---
echo ">> writing manifest.json"
kernel_sha="$(sha256sum "$dist_dir/vmlinux" | awk '{print $1}')"
rootfs_sha="$(sha256sum "$dist_dir/rootfs.ext4" | awk '{print $1}')"
rootfs_size="$(stat -c%s "$dist_dir/rootfs.ext4")"
built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
builder="${USER:-unknown}@$(hostname)"

cat > "$dist_dir/manifest.json" <<EOF
{
  "kernel_sha256": "$kernel_sha",
  "rootfs_sha256": "$rootfs_sha",
  "built_at": "$built_at",
  "builder": "$builder",
  "kernel_version": "$kernel_version",
  "rootfs_size_bytes": $rootfs_size
}
EOF

# --- 4. Optional signing ---
if [[ -n "${GEMBA_RELEASE_KEY:-}" ]]; then
  echo ">> signing manifest with $GEMBA_RELEASE_KEY"
  # ed25519 raw signature over the manifest bytes. openssl 3.x supports
  # this via -rawin; older systems should upgrade or pre-hash.
  openssl pkeyutl -sign -inkey "$GEMBA_RELEASE_KEY" -rawin \
    -in "$dist_dir/manifest.json" -out "$dist_dir/manifest.sig"
fi

echo ">> done: $dist_dir"
ls -l "$dist_dir"
