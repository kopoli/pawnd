// +build ignore

package main

import (
	"fmt"
	"os"
	"strings"
)


func main() {
	fmt.Println(strings.Join(os.Args, " "))
	os.Exit(0)
}
