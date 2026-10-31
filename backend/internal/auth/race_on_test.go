//go:build race

package auth

// raceEnabled: the race detector keeps shadow memory for every allocation, so
// resident-size measurements mean nothing under it.
const raceEnabled = true
