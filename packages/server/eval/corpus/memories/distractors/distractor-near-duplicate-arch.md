## Memax System Architecture Overview

Memax is a universal context and memory hub for AI agents. The architecture consists of a Python FastAPI server with MongoDB for storage, OpenAI text-embedding-ada-002 for embeddings, and GPT-4 for LLM operations. The monorepo contains packages for the API server, web frontend (React with Vite), CLI, SDK, and documentation site.

Key components: The retrieval pipeline uses FAISS for vector similarity search combined with Elasticsearch for full-text matching. Background jobs run on Celery with Redis as the broker. Authentication uses Firebase Auth with JWT tokens. The web app deploys to AWS Amplify and the API server runs on AWS ECS.

Storage layers: MongoDB stores memory documents, Redis caches embeddings and session data, and S3 handles object storage for large attachments. The CLI publishes to npm as `memax-cli`.
