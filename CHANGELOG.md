# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog,
and this project follows Semantic Versioning.

## [0.1.0] - 2026-03-14

### Added

- Go + Gin backend with OpenAI-compatible chat endpoints
- Ollama native API support (`/api/chat`, `/api/embed`)
- Qdrant integration: collection lifecycle, upsert, retrieval
- RAG pipeline: chunking, embedding, retrieval, prompt context injection
- Retrieval improvements v1:
  - Dynamic TopK
  - Candidate rerank by keyword coverage
  - MMR diversity selection
  - Low-confidence fallback with expanded recall
- Embedding cache: in-memory thread-safe LRU (2048 entries)
- App state persistence (`backend/data/app-state.json`)
- React + Vite frontend: chat UI, knowledge panel, settings panel
- Docker deployment: frontend + backend + qdrant compose
- Tests: unit + e2e (including retrieval strategy tests)

### Notes

- Current release is intended for local/self-hosted usage.
- Performance benchmark and launch scripts are planned in next iteration.
