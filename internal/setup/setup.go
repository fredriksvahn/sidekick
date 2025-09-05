package setup

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/ollama"
)

func promptPick(prompt string, options []string) string {
	fmt.Println(prompt)
	for i, o := range options {
		fmt.Printf("  [%d] %s\n", i+1, o)
	}
	fmt.Print("Select number: ")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		s := strings.TrimSpace(in.Text())
		var idx int
		_, err := fmt.Sscanf(s, "%d", &idx)
		if err == nil && idx >= 1 && idx <= len(options) {
			return options[idx-1]
		}
		fmt.Print("Try again: ")
	}
	return ""
}

func Run(hostname string) error {
	cfg, _ := config.Load() // ignore if missing
	// list models
	models, err := ollama.ListLocalModels("http://localhost:11434")
	if err != nil || len(models) == 0 {
		fmt.Println("No local models detected via ollama. Is ollama running?")
	}

	// fallbacks if nothing found
	if len(models) == 0 {
		models = []string{"phi3:3.8b", "qwen2.5:3b", "mistral:7b", "deepseek-coder:1.3b", "deepseek-coder:6.7b"}
	}

	gen := promptPick("Pick GENERAL model:", models)
	code := promptPick("Pick CODE model:", models)

	// build profile with sensible knobs
	p := config.DefaultProfile()
	if gen != "" { p.GeneralModel = gen }
	if code != "" { p.CodeModel = code }

	config.SetHostProfile(&cfg, hostname, p)
	if cfg.DefaultHost == "" { cfg.DefaultHost = hostname }

	if err := config.Save(cfg); err != nil { return err }
	fmt.Printf("Saved config to %s (host=%s)\n", config.File(), hostname)
	return nil
}

