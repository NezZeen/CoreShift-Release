// Command graceful waits for an interrupt and then writes the file named by
// its argument, proving it got the chance to clean up.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	fmt.Println("ready")
	<-c
	os.WriteFile(os.Args[1], []byte("clean"), 0o644)
}
