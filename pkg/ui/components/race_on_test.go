//go:build race

package components

// raceEnabled relaxes timing assertions: the race detector slows code 5-20x.
const raceEnabled = true
