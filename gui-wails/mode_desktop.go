//go:build !server

package main

import "log"

func logMode(agent string) {
	log.Printf("Tailfile Wails desktop window (agent %s)", agent)
}
