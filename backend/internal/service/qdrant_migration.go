package service

import (
	"context"
	"fmt"
	"strings"

	"ai-localbase/internal/model"
)

func MigrateQdrantPayloads(
	ctx context.Context,
	source *QdrantService,
	target *QdrantService,
	rag *RagService,
	embeddingConfig model.EmbeddingModelConfig,
	knowledgeBaseID string,
) (int, error) {
	if source == nil || !source.IsEnabled() {
		return 0, fmt.Errorf("source qdrant service is not configured")
	}
	if target == nil || !target.IsEnabled() {
		return 0, fmt.Errorf("target qdrant service is not configured")
	}
	if rag == nil {
		return 0, fmt.Errorf("rag service is not configured")
	}
	if strings.TrimSpace(knowledgeBaseID) == "" {
		return 0, fmt.Errorf("knowledge base id is required")
	}

	storedPoints, err := source.ScrollPointPayloads(ctx, knowledgeBaseID)
	if err != nil {
		return 0, fmt.Errorf("read source qdrant payloads: %w", err)
	}
	if len(storedPoints) == 0 {
		return 0, nil
	}

	texts := make([]string, 0, len(storedPoints))
	pointsWithText := make([]QdrantStoredPoint, 0, len(storedPoints))
	for _, point := range storedPoints {
		text := strings.TrimSpace(payloadString(point.Payload, "text", ""))
		if text == "" {
			continue
		}
		texts = append(texts, text)
		pointsWithText = append(pointsWithText, point)
	}
	if len(texts) == 0 {
		return 0, fmt.Errorf("source qdrant collection contains no text payloads")
	}

	vectors, err := rag.EmbedTexts(ctx, embeddingConfig, texts, target.vectorSize)
	if err != nil {
		return 0, fmt.Errorf("embed source qdrant payloads: %w", err)
	}
	if len(vectors) != len(pointsWithText) {
		return 0, fmt.Errorf("embedding response count mismatch: expected %d, got %d", len(pointsWithText), len(vectors))
	}
	if err := target.EnsureCollection(ctx, knowledgeBaseID); err != nil {
		return 0, fmt.Errorf("ensure target qdrant collection: %w", err)
	}

	targetPoints := make([]QdrantPoint, 0, len(pointsWithText))
	for index, point := range pointsWithText {
		text := texts[index]
		targetPoints = append(targetPoints, QdrantPoint{
			ID:      point.ID,
			Vector:  qdrantPointVectors(vectors[index], BuildSparseVector(text)),
			Payload: clonePayload(point.Payload),
		})
	}
	if err := target.UpsertPoints(ctx, knowledgeBaseID, targetPoints); err != nil {
		return 0, fmt.Errorf("write target qdrant payloads: %w", err)
	}
	return len(targetPoints), nil
}
