// Command cpudefect burns CPU in a quadratic loop and writes a CPU profile of it. It is a separate program because the
// race detector breaks CPU stack unwinding, and the gate runs the tests with -race.
package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"
)

const burn = 700 * time.Millisecond

// hasDuplicates compares every pair: quadratic work that looks harmless on a small slice.
//
//go:noinline
func hasDuplicates(items []int) bool {
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if items[i] == items[j] {
				return true
			}
		}
	}
	return false
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cpudefect <profile-file>")
		os.Exit(2)
	}
	out, err := os.Create(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = out.Close() }()

	if err = pprof.StartCPUProfile(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	items := make([]int, 3000)
	for i := range items {
		items[i] = i
	}
	for start := time.Now(); time.Since(start) < burn; {
		hasDuplicates(items)
	}
	pprof.StopCPUProfile()
}
