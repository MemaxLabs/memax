## MongoDB Atlas Setup Guide

Setting up MongoDB Atlas for the platform's document store. We chose MongoDB for its flexible schema — documents like user profiles and activity logs don't need rigid table structures.

**Cluster configuration:**
- Tier: M30 dedicated (us-east-1)
- Replica set: 3 nodes with auto-failover
- Storage: 100 GB NVMe SSD, encrypted at rest

**Connection:** Use the SRV connection string from the Atlas dashboard. Set `retryWrites=true` and `w=majority` for durability. Connection pooling: max 50 connections per application server.

**Indexes:** Create compound indexes on `{ user_id: 1, created_at: -1 }` for the activity collection. Text index on `{ title: "text", body: "text" }` for full-text search.

**Backup:** Atlas automated daily snapshots with 7-day retention. Point-in-time recovery enabled.

Note: For vector search, we'd need to integrate Atlas Vector Search (Preview) or use a separate pgvector instance alongside Mongo.
