package setup

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
)

func promptPick(prompt string, options []string, def string) string {
	fmt.Printf("%s (default: %s)\n", prompt, def)
	for i, o := range options {
		fmt.Printf("  [%d] %s\n", i+1, o)
	}
	fmt.Print("Select number: ")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		s := strings.TrimSpace(in.Text())
		if s == "" {
			return def
		}
		var idx int
		_, err := fmt.Sscanf(s, "%d", &idx)
		if err == nil && idx >= 1 && idx <= len(options) {
			return options[idx-1]
		}
		fmt.Print("Try again: ")
	}
	return def
}

func promptFloat(prompt string, def float64) float64 {
	fmt.Printf("%s (default: %.2f): ", prompt, def)
	in := bufio.NewScanner(os.Stdin)
	if in.Scan() {
		s := strings.TrimSpace(in.Text())
		if s == "" {
			return def
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return def
}

func promptInt(prompt string, def int) int {
	fmt.Printf("%s (default: %d): ", prompt, def)
	in := bufio.NewScanner(os.Stdin)
	if in.Scan() {
		s := strings.TrimSpace(in.Text())
		if s == "" {
			return def
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func Run(hostname string) error {
	cfg, _ := config.Load()

	models := []string{"gpt-4o-mini", "gpt-4o"}

	p := config.DefaultProfile()
	p.Model = promptPick("Pick default OpenAI model:", models, p.Model)
	p.Temperature = promptFloat("Default temperature", p.Temperature)
	p.MaxTokens = promptInt("Default max tokens", p.MaxTokens)

	config.SetHostProfile(&cfg, hostname, p)
	if cfg.DefaultHost == "" {
		cfg.DefaultHost = hostname
	}

	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("Saved config to %s (host=%s)\n", config.File(), hostname)
	return nil
}
