package ollama

import (
	"encoding/json"
	"net/http"
)

type TagResponse struct {
	Models []struct {
		Model string `json:"model"`
	} `json:"models"`
}

func ListLocalModels(host string) ([]string, error) {
	resp, err := http.Get(host + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tr TagResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		out = append(out, m.Model)
	}
	return out, nil
}
