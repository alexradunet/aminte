//go:build !race

package view

// raceDetector says that the tests run under the race detector (race_test.go); here they do not.
const raceDetector = false
