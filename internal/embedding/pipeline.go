package embedding

import (
	"context"
	"sync"
	"time"
)

// Pipeline collects concurrent embedding requests into batches.
type Pipeline struct {
	embedder  Embedder
	batchSize int
	timeout   time.Duration

	mu      sync.Mutex
	pending []pipelineRequest
	timer   *time.Timer

	closeCh chan struct{}
	wg      sync.WaitGroup
}

type pipelineRequest struct {
	text   string
	result chan<- pipelineResult
}

type pipelineResult struct {
	embedding []float32
	err       error
}

// NewPipeline creates a batching pipeline for embeddings.
func NewPipeline(embedder Embedder, batchSize int, timeout time.Duration) *Pipeline {
	if batchSize <= 0 {
		batchSize = 64
	}
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}
	p := &Pipeline{
		embedder:  embedder,
		batchSize: batchSize,
		timeout:   timeout,
		closeCh:   make(chan struct{}),
	}
	return p
}

// Embed submits a single text for embedding and waits for the result.
func (p *Pipeline) Embed(ctx context.Context, text string) ([]float32, error) {
	ch := make(chan pipelineResult, 1)

	p.mu.Lock()
	p.pending = append(p.pending, pipelineRequest{text: text, result: ch})

	if len(p.pending) >= p.batchSize {
		batch := p.pending
		p.pending = nil
		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		p.mu.Unlock()
		p.processBatch(ctx, batch)
	} else {
		if p.timer == nil {
			p.timer = time.AfterFunc(p.timeout, func() {
				p.mu.Lock()
				batch := p.pending
				p.pending = nil
				p.timer = nil
				p.mu.Unlock()
				if len(batch) > 0 {
					p.processBatch(context.Background(), batch)
				}
			})
		}
		p.mu.Unlock()
	}

	select {
	case r := <-ch:
		return r.embedding, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Pipeline) processBatch(ctx context.Context, batch []pipelineRequest) {
	texts := make([]string, len(batch))
	for i, r := range batch {
		texts[i] = r.text
	}

	embeddings, err := p.embedder.Embed(ctx, texts)
	for i, r := range batch {
		if err != nil {
			r.result <- pipelineResult{err: err}
		} else if i < len(embeddings) {
			r.result <- pipelineResult{embedding: embeddings[i]}
		} else {
			r.result <- pipelineResult{err: context.Canceled}
		}
	}
}

// Close drains pending requests and stops the pipeline.
func (p *Pipeline) Close() {
	close(p.closeCh)
	p.mu.Lock()
	if p.timer != nil {
		p.timer.Stop()
	}
	// Process any remaining.
	batch := p.pending
	p.pending = nil
	p.mu.Unlock()

	if len(batch) > 0 {
		p.processBatch(context.Background(), batch)
	}
}

// Dimensions returns the embedding dimensions.
func (p *Pipeline) Dimensions() int {
	return p.embedder.Dimensions()
}
