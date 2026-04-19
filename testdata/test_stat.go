package main

import 
	"fmt"
	"os"
    "os"
m
)
	info, err := os.Stat("hello.c")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Found:", info.Name(), info.Size())
	}
    }
}
