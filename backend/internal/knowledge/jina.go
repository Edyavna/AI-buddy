package knowledge

import (
	"context"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/providerhttp"
	"os"
)

type Jina struct{ Client providerhttp.Client }

func JinaFromEnv() *Jina {
	return &Jina{Client: providerhttp.Client{BaseURL: os.Getenv("EMBEDDING_BASE_URL"), Key: os.Getenv("EMBEDDING_API_KEY")}}
}
func (j *Jina) Embed(ctx context.Context, task, text string) ([]float64, error) {
	var r struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if task != "retrieval.query" && task != "retrieval.passage" {
		return nil, ports.ErrInvalid
	}
	e := j.Client.Post(ctx, "/embeddings", map[string]any{"model": "jina-embeddings-v3", "task": task, "dimensions": 1024, "input": []string{text}, "encoding_format": "float"}, &r)
	if e != nil {
		return nil, e
	}
	if len(r.Data) != 1 {
		return nil, ports.ErrUnavailable
	}
	if _, e = vector(r.Data[0].Embedding); e != nil {
		return nil, e
	}
	return r.Data[0].Embedding, nil
}
