package main

import (
	"errors"
	"fmt"
	"os"
)

// BuggyLoad has missing error handling — a seeded HIGH finding.
func BuggyLoad(path string) string {
	data, _ := os.ReadFile(path) // error ignored
	return string(data)
}

func main() {
	fmt.Println(BuggyLoad("config.txt"))
	_ = errors.New("unused error")
}
