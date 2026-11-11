package knowledge

import (
	"context"
	"os"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
)

type Jina struct{ Client providerhttp.Client }

func JinaFromEnv() *Jina {
	return &Jina{Client: providerhttp.Client{BaseURL: os.Getenv("EMBEDDING_BASE_URL"), Key: os.Getenv("EMBEDDING_API_KEY")}}
}

func (j *Jina) Embed(ctx context.Context, task, text string) ([]float64, error) {
	items, err := j.EmbedBatch(ctx, task, []string{text})
	if err != nil {
		return nil, err
	}
	return items[0], nil
}

func (j *Jina) EmbedBatch(ctx context.Context, task string, texts []string) ([][]float64, error) {
	var r struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if (task != "retrieval.query" && task != "retrieval.passage") || len(texts) == 0 {
		return nil, ports.ErrInvalid
	}
	err := j.Client.Post(ctx, "/embeddings", map[string]any{"model": "jina-embeddings-v3", "task": task, "dimensions": 1024, "input": texts, "encoding_format": "float"}, &r)
	if err != nil {
		return nil, err
	}
	if len(r.Data) != len(texts) {
		return nil, ports.ErrUnavailable
	}
	items := make([][]float64, len(texts))
	for _, item := range r.Data {
		if item.Index < 0 || item.Index >= len(items) || items[item.Index] != nil {
			return nil, ports.ErrUnavailable
		}
		if _, err := vector(item.Embedding); err != nil {
			return nil, err
		}
		items[item.Index] = item.Embedding
	}
	return items, nil
}
