package main

import (
	"fmt"

	"example.com/review/store"
)

func main() {
	var s store.Saver = &store.Disk{}
	run(s)
	fmt.Println(Update(1), endpoint())
}

func run(s store.Saver) { s.Save() }

// Update adds one. Its body sits on the definition line.
func Update(n int) int { return n + 1 }

func unusedHelper() {
}

type buffer struct{}

func (b *buffer) Flush() {
	// TODO: flush the pending writes
}

func (b buffer) Size() int { return 0 }

func endpoint() string {
	url := "http://localhost:8080/v1"
	return url
}

func init() {
	// FIXME: read the config here
}
