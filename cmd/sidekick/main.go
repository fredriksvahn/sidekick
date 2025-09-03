package main

import (
	"fmt"
	"os"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/ollama"
	"github.com/earlysvahn/sidekick/internal/router"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: sidekick \"your question\"")
		os.Exit(1)
	}
	question := os.Args[1]

	appCfg := config.FromEnv()
	routedQ, ollamaCfg := router.Route(question, appCfg)

	answer := ollama.Ask(ollamaCfg, routedQ)
	fmt.Println(answer)
}

