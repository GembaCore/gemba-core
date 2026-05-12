################################################################################
# gemba-vm-init — guest-side init for Gemba microVMs.
#
# This package builds the static Go binary at vm-image/init/ and installs
# it at /sbin/gemba-vm-init in the rootfs. The guest /sbin/init symlink
# (set up in board/gemba/rootfs-overlay) points at this binary.
#
# We deliberately build the Go binary with the *host* Go toolchain, not
# Buildroot's. The init binary is fully static (CGO_ENABLED=0) and
# Linux/amd64-only, so cross-compiling from any host is trivial. This
# keeps the Buildroot story decoupled from BR2_PACKAGE_GO_BIN_LINUX_4_x
# version pinning.
################################################################################

GEMBA_VM_INIT_VERSION = 0.1.0
GEMBA_VM_INIT_SITE = $(BR2_EXTERNAL_GEMBA_PATH)/../../init
GEMBA_VM_INIT_SITE_METHOD = local
GEMBA_VM_INIT_LICENSE = Apache-2.0
GEMBA_VM_INIT_LICENSE_FILES = LICENSE

# We invoke the host Go toolchain directly rather than going through
# Buildroot's package-go infrastructure — see the package header above.
define GEMBA_VM_INIT_BUILD_CMDS
	cd $(@D) && \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		$(HOST_DIR)/bin/go build -tags init -trimpath \
		-ldflags "-s -w -X main.version=$(GEMBA_VM_INIT_VERSION)" \
		-o $(@D)/gemba-vm-init .
endef

define GEMBA_VM_INIT_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/gemba-vm-init \
		$(TARGET_DIR)/sbin/gemba-vm-init
	ln -sf gemba-vm-init $(TARGET_DIR)/sbin/init
endef

$(eval $(generic-package))
