# go-api

Go/Chi API for the Toronto short-term rental map.

## Configuration

Required:

- `SUPABASE_URL`: Postgres/Supabase connection string.

Optional:

- `PORT`: API port, defaults to `8080`.
- `CORS_ALLOWED_ORIGINS`: comma-separated frontend origins.
- `INTERNAL_API_KEY`: when set, `/api/*` routes require a matching `X-Internal-API-Key` header from the Next.js proxy. Use the same value in the frontend deployment.
