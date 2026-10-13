package service

import (
	"encoding/json"

	"github.com/Gulugll/fupan/internal/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		Dev: true, DataDir: "t", Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2,
		LLM: config.LLMConfig{BaseURL: "http://unused", ChatModel: "glm-4", EmbedModel: "embedding-3"},
	}
}

func jsonMarshal(s string) ([]byte, error) { return json.Marshal(s) }
