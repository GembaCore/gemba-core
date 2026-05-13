# Gemba microVM kernel

Linux **6.1 LTS** (the Firecracker-recommended baseline).

Upstream source: <https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.1.tar.xz>

## Build

The kernel is built outside Buildroot — Buildroot owns only the rootfs.
See `docs/server/vm-image.md` for the full procedure. In short:

```bash
curl -O https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.1.tar.xz
tar xf linux-6.1.tar.xz
cd linux-6.1
cp ../gemba_kernel.config .config
make olddefconfig
make -j"$(nproc)" vmlinux
cp vmlinux ../../dist/vm/vmlinux
```

## Why a custom config

Firecracker's docs ship a "microvm" config that drops every driver
not strictly required: no framebuffer, no sound, no USB, no SATA, no
keyboard. Boot time on a stock distro kernel is ~3s. With the
microvm-tuned config, the kernel finishes its bring-up in **under
125ms**. That's the whole point of using Firecracker over a VM monitor
that boots a full kernel.

`gemba_kernel.config` is essentially Firecracker's recommended config
with the following adds:

- `CONFIG_VIRTIO_FS=y` — for the `/workspace` pass-through.
- `CONFIG_FUSE_FS=y` — virtio-fs depends on it.
- `CONFIG_OVERLAY_FS=y` — agent containers may want overlay mounts.
- `CONFIG_SECCOMP=y`, `CONFIG_SECCOMP_FILTER=y` — for agent-side
  sandboxing inside the VM.
- `CONFIG_PRINTK_TIME=y` — boot-time tracing in our logs.

## Why 6.1 specifically

- Firecracker's CI matrix tests against 5.10 and 6.1; we pick the
  newer one so we get virtio-fs without backports.
- LTS through 2026 (per kernel.org), so we don't have to roll the
  base every quarter.
- The Gemba runtime depends on `clone3` and `openat2` — both stable
  on 5.x but with steady fixes through 6.x.

## Updating

Bumping the kernel is a deliberate, reviewed change because every
existing manifest.json hash invalidates. Procedure:

1. Update the URL + config in this directory.
2. Rebuild on a Linux host (`make vm-image`).
3. Sign the new manifest (`GEMBA_RELEASE_KEY=… make vm-image`).
4. Ship as a new release.
5. The runtime VerifyImage refuses to boot the new kernel against an
   old manifest — that's intentional.
