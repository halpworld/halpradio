//go:build race

package dsp

// raceEnabled relaxes timing assertions: the race detector slows code 5-20x.
const raceEnabled = true
