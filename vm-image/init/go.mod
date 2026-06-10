// gemba-vm-init is a standalone Go module so it can be cross-compiled
// from any host without dragging in the main gemba module's deps. It's
// Linux-only (the binary is PID 1 inside a Firecracker microVM) and
// uses only stdlib.
module github.com/GembaCore/gemba/vm-image/init

go 1.22
