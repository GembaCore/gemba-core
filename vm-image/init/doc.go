// Package main (gemba-vm-init) is the PID 1 binary inside a Gemba
// microVM. See main.go for the full lifecycle. This file exists so
// `go doc ./vm-image/init/` and `go vet` on non-Linux hosts have
// something to chew on without pulling in the build-tagged source.
package main
