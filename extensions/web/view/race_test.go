//go:build race

package view

// raceDetector says that the tests run under the race detector, whose instrumentation makes every allocation larger:
// a test that counts bytes as a measure of work does not hold under it.
const raceDetector = true
