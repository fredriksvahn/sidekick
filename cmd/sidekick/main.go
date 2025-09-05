package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/ollama"
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
	flag.StringVar(&modelOverride, "model", "", "force a specific model")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Println("Usage: sidekick [--model MODEL] \"your prompt\"")
		os.Exit(1)
	}
	prompt := flag.Arg(0)

	// clean: just resolve config
	cfg := config.Resolve(modelOverride)

	// ask Ollama
	reply := ollama.Ask(cfg.Ollama, prompt)
	fmt.Println(reply)
}
