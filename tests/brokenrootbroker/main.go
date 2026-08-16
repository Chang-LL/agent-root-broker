package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("rootbroker system-broken")
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "rootbrokerd" && os.Args[2] == "--check-config" {
		return
	}
	fmt.Fprintln(os.Stderr, "intentional system-test daemon failure")
	os.Exit(1)
}
