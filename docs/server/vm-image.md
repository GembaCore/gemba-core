# VM image: Firecracker kernel + rootfs

> **Bead**: gm-o9t8.3.2.2
> **Status**: build pipeline live; reference image not yet baked.

Gemba's remote dispatch path runs every agent run in a per-job
Firecracker microVM. That microVM needs two artifacts:

| File | Role |
|---|---|
| `vmlinux` | A Linux 6.1 kernel built with a Firecracker-tuned config (no framebuffer, no audio, virtio-fs enabled). |
| `rootfs.ext4` | A minimal Buildroot rootfs with busybox, `git`, CA certs, and `/sbin/gemba-vm-init` (PID 1). |

A third file, `manifest.json`, sits next to them and records sha256
hashes plus build metadata. The Gemba server verifies these hashes
before booting any VM.

```
~/.gemba/vm/
├── vmlinux
├── rootfs.ext4
├── manifest.json
└── manifest.sig   # optional (ed25519 signature over manifest.json)
```

## How to build

The bake is **Linux-only**. macOS / Windows are fine for editing
Buildroot config and the init binary, but you cannot run Buildroot or
build the Linux kernel without a Linux host. `make vm-image` on macOS
prints a friendly skip message and exits 0 — that's by design so the
top-level make never breaks for non-server contributors.

### Prerequisites (Linux host)

- Buildroot 2024.x (or newer) checkout
- Linux 6.1.x source tree (unpacked)
- Standard build deps: `gcc`, `make`, `bc`, `flex`, `bison`, `libssl-dev`, `libelf-dev`
- Go 1.22+ (host toolchain — cross-compiles the init binary)

### One-shot build

```bash
# Inside a Linux host with the gemba repo cloned:
export BUILDROOT_DIR=/opt/buildroot-2024.02
export KERNEL_SRC_DIR=/opt/linux-6.1.123
make vm-image
```

Outputs land at `dist/vm/`:

```
dist/vm/vmlinux
dist/vm/rootfs.ext4
dist/vm/manifest.json
```

`make vm-image-clean` removes the dist outputs.

### What runs

`make vm-image` shells out to `vm-image/scripts/build.sh`:

1. Copies `vm-image/kernel/gemba_kernel.config` over the kernel
   source tree's `.config`, runs `make olddefconfig` + `make vmlinux`.
2. Drives Buildroot with `BR2_EXTERNAL=vm-image/buildroot/external`
   and `BR2_DEFCONFIG=vm-image/buildroot/gemba_defconfig`. The
   external tree contributes the `gemba-vm-init` package, which
   cross-compiles the Go binary in `vm-image/init/` with
   `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.
3. Computes sha256s and writes `dist/vm/manifest.json` against this
   schema:

   ```json
   {
     "kernel_sha256": "<64-char hex>",
     "rootfs_sha256": "<64-char hex>",
     "built_at": "<RFC3339>",
     "builder": "<user>@<host>",
     "kernel_version": "6.1.123",
     "rootfs_size_bytes": 268435456
   }
   ```

4. If `GEMBA_RELEASE_KEY` is set to the path of an ed25519 private
   key (PEM), runs `openssl pkeyutl -sign -rawin` to produce
   `dist/vm/manifest.sig`.

## How to ship the artifacts

The artifacts ride along with each Gemba server release as a separate
tarball, not bundled into the main `gemba` binary (they're ~50–80 MB
combined and don't need to update on every release).

Release flow (CI):

1. CI Linux runner executes `make vm-image` and `GEMBA_RELEASE_KEY=…
   make vm-image` for production releases.
2. The runner uploads `dist/vm/vmlinux`, `dist/vm/rootfs.ext4`,
   `dist/vm/manifest.json`, and `dist/vm/manifest.sig` to the GitHub
   release as `gemba-vm-image-vX.Y.Z.tar.gz`.

## Where users put them on their server

Operators unpack the tarball into one of these well-known locations
(checked in this order by `firecracker.DefaultImagePaths`):

1. `$GEMBA_VM_DIR` (explicit override)
2. `~/.gemba/vm/` (single-user install)
3. `/usr/local/share/gemba/vm/` (system-wide install)

Example:

```bash
sudo mkdir -p /usr/local/share/gemba/vm
sudo tar -xzf gemba-vm-image-v1.4.0.tar.gz -C /usr/local/share/gemba/vm
ls /usr/local/share/gemba/vm
# vmlinux  rootfs.ext4  manifest.json  manifest.sig
```

## The verification model

Every VM boot calls `firecracker.VerifyImage(kernel, rootfs, manifest)`
before invoking the Firecracker API. The function:

1. Parses `manifest.json` and rejects non-hex digests up front.
2. Re-hashes the kernel and rootfs files on disk.
3. Returns an error if either digest doesn't match.

A non-nil return is a hard refusal — the VM never boots. This protects
against:

- **Partial downloads**: incomplete release tarball during install.
- **Disk corruption**: bit-flip on the server's storage.
- **Casual tampering**: someone editing the rootfs in place.

The optional `manifest.sig` (ed25519 over the manifest bytes) covers
the **supply-chain** angle: it lets an operator verify that a manifest
they received was actually published by the Gemba release pipeline.
v1 ships the schema + signing step but does not yet enforce signature
verification at boot — that's tracked in a follow-up bead.

## The macOS / Windows fallback

`internal/adapter/firecracker/firecracker_fallback.go` provides a
subprocess-based stand-in for dev machines. It does **not** call
`VerifyImage`, does **not** require any of these artifacts, and is
explicitly not a security boundary. It exists so that the rest of the
remote-dispatch code path compiles and runs in a normal dev loop.

Production Linux servers always take the Firecracker path; the
fallback never runs on a real Gemba server install.

## Source tree layout

```
vm-image/
├── buildroot/
│   ├── gemba_defconfig          # rootfs Buildroot config
│   └── external/                # BR2_EXTERNAL tree
│       ├── external.desc
│       ├── external.mk
│       ├── Config.in
│       ├── package/gemba-vm-init/  # PID-1 init package
│       └── board/gemba/rootfs-overlay/  # /etc stubs
├── init/                        # gemba-vm-init Go source (Linux-only)
├── kernel/
│   ├── gemba_kernel.config      # Linux 6.1 microvm config
│   └── README.md                # upstream pointer + rationale
└── scripts/
    └── build.sh                 # orchestrator invoked by `make vm-image`
```

## Updating the image

Kernel and rootfs bumps both invalidate every existing `manifest.json`
hash — that's intentional. To roll a new image:

1. Edit the relevant config (`gemba_kernel.config`,
   `gemba_defconfig`, or `vm-image/init/`).
2. Rebuild on a Linux host: `make vm-image`.
3. Sign for release: `GEMBA_RELEASE_KEY=… make vm-image`.
4. Cut a Gemba release; the new tarball ships with it.
5. Operators must re-install the new tarball — old manifests will
   fail `VerifyImage`.
