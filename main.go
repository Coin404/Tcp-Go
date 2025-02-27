package main

import (
	"fmt"

	"time"
)

func init() {
	fmt.Println("Main Init : ", time.Now().Format("2006-01-02 15:04:05"))
}
