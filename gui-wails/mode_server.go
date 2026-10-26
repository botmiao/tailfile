//go:build server

package main

import "log"

func logMode(agent string) {
	log.Printf("Tailfile Wails server http://127.0.0.1:%d  (agent %s)", serverPort, agent)
}
