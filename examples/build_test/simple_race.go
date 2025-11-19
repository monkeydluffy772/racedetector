package main

import (
	"fmt"
	"time"
)

func main() {
	var x int

	// Goroutine 1: Write to x
	go func() {
		x = 1
	}()

	// Goroutine 2: Read from x (potential race!)
	go func() {
		fmt.Println("x =", x)
	}()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Done!")
}
