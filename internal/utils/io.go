package utils

import (
	"encoding/json"
	"os"
)

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ReadJSON[T any](path string, out *T) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
