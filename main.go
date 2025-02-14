package main

import (
	"fmt"

	"time"
)

// 初始化函数 golang 每个包的引用会有限调用该函数
func init() {
	fmt.Println("Main Init : ", time.Now().Format("2006-01-02 15:04:05"))
}
