// Package integration hosts cross-cutting tests that wire pipeline +
// lifecycle + locker + adapters together — the same composition the GUI
// composition root performs, minus Wails. Production has no callers; the
// package only exists so the test binary has a home.
package integration
