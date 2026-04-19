package main

import (
	"fmt"
	"os"
)

func main() {
	info, err := os.Stat("hello.c")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Found:", info.Name(), info.Size())
	}
}
