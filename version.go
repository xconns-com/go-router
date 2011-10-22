package main

import (
	"fmt"
	"go-router.googlecode.com/svn/trunk"
)

func main() {
	fmt.Println("stable version")
	r := router.New(router.IntID(), 32, router.BroadcastPolicy)
	r.Close()
}
