package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/openai"
	"github.com/earlysvahn/sidekick/internal/router"
	"github.com/earlysvahn/sidekick/internal/setup"
)

func main() {
	// special case: setup
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		h, _ := os.Hostname()
		if err := setup.Run(h); err != nil {
			fmt.Fprintln(os.Stderr, "setup error:", err)
			os.Exit(1)
		}
		return
	}

	var modelOverride string
	flag.StringVar(&modelOverride, "model", "", "force a specific OpenAI model")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Println("Usage: sidekick [--model MODEL] \"your prompt\"")
		os.Exit(1)
	}
	rawPrompt := strings.Join(flag.Args(), " ")

	cfg := config.Resolve(modelOverride, rawPrompt)

	prompt, routedCfg := router.Route(rawPrompt, cfg)
	reply, err := openai.Ask(routedCfg.OpenAI, prompt)

	if err != nil {
		fmt.Fprintln(os.Stderr, "[openai error]", err)
		os.Exit(1)
	}
	fmt.Println(reply)
}
