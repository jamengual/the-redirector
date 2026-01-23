package metrics

import (
	"runtime"
)

// getGoroutineCount returns the current number of goroutines.
func getGoroutineCount() int {
	return runtime.NumGoroutine()
}

// getMemoryAlloc returns the current memory allocation in bytes.
func getMemoryAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}
