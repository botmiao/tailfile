package main

import (
	"flag"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"tailfile.local/agent/guitoken"
)

func main() {
	addr := flag.String("agent", "http://127.0.0.1:8645", "local tailfile agent")
	tokenFile := flag.String("token-file", "", "path to the agent GUI token (default: the state dir gui-token file)")
	stateDir := flag.String("state", "", "agent state dir used to find gui-token")
	flag.Parse()

	token, err := guitoken.Load(*tokenFile, *stateDir)
	if err != nil {
		log.Printf("tailfile: %v", err)
		token = ""
	}
	a := newApp(*addr, token)
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tailfile",
			Width:           1200,
			Height:          780,
			MinWidth:        860,
			MinHeight:       520,
			BackgroundColor: "light-dark(#f4f4f5, #1c1c1e)",
			Content:         ui.View(a.view),
		})
		a.win.OnClosed(func() { close(a.stopPoll) })
		a.refresh()
		a.startPoll()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
