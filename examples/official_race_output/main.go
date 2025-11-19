package main

import (
	"fmt"
	"time"
)

var counter int

func increment() {
	counter++ // Race: concurrent write without synchronization
}

func main() {
	// Start two goroutines that race on 'counter'
	go increment()
	go increment()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("Counter:", counter)
}
