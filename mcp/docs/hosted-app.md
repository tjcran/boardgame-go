# Hosted mode: boardgame-mcp as a remote Claude connector

This is the HTTP/JWT-OAuth mode of `boardgame-mcp`. Deploy the binary once, expose it as an MCP connector on Claude.ai (or any other MCP-capable client), and consumers add it to their account with no install required.

For local stdio play, see `local-mcp.md`.

## What you ship

One Go binary speaking JSON-RPC 2.0 / MCP 2024-11-05 over HTTP. Six tools, one prompt (`play-tictactoe`), `/healthz` for Cloud Run probes. Optional OAuth 2.1 JWT verification scopes matches per authenticated user; without it the deployment is single-tenant.

## Build the image

```sh
docker build -t boardgame-mcp -f mcp/deploy/Dockerfile .
```

Multi-stage build: `golang:1.24-alpine` → `distroless/static:nonroot`. Final image is ~15 MB, runs as nonroot, no shell, no package manager. `CGO_ENABLED=0` keeps the binary fully static.

## Deploy to Cloud Run

The sample spec at `mcp/deploy/cloudrun.yaml` configures scale-to-zero (`minScale: 0`), 1 CPU + 256 MiB, and `/healthz` start+liveness probes. Replace `PROJECT`, `VERSION`, `JWKS_URL`, `ISSUER`, and `AUDIENCE` before applying:

```sh
gcloud run services replace mcp/deploy/cloudrun.yaml --region=us-central1
gcloud run services add-iam-policy-binding boardgame-mcp \
    --member=allUsers --role=roles/run.invoker --region=us-central1
```

Cost ceiling for an idle deployment: ~$0/month (scale-to-zero + free tier). Set a billing alert at $20/month before opening it up publicly.

## Configure OAuth (production)

The MCP-spec OAuth 2.1 flow ends with the connector handing your server a Bearer JWT issued by the user's Claude account (or another OIDC issuer). Configure the verifier:

```sh
boardgame-mcp serve --transport=http --port=8080 \
    --jwks-url=https://issuer/.well-known/jwks.json \
    --issuer=https://issuer/ \
    --audience=boardgame-mcp
```

What gets verified:

- **JWT signature** against the issuer's JWKS (RS256 and ES256 only — HS-family algorithms are rejected unconditionally).
- **`iss`** must equal `--issuer`.
- **`aud`** must include `--audience` (string or array form both accepted).
- **`exp`/`nbf`** with a 30s clock skew tolerance.
- **`sub`** must be present; becomes the userID used to scope matches.

When auth is enabled, `Tools.Ownership` is set: every match is owned by its creator's `sub`, and every other tool with a `matchID` arg refuses to act unless the caller is that owner. This is the multi-tenant property — user A's matches are completely invisible to user B.

Without `--jwks-url`, the HTTP server runs single-tenant: every request shares the same data under the userID `anonymous`. Fine for dev / single-user deployments; never expose this configuration publicly.

## Register as a Claude.ai connector

The exact connector-registration UI evolves with Claude.ai; the durable shape is:

1. Open Claude.ai → Settings → Connectors → Add custom connector.
2. Server URL: `https://your-cloud-run-url/mcp`.
3. Authentication: OAuth 2.1 (Anthropic-issued token).
4. The connector handshakes via the standard MCP `initialize` / `tools/list` flow.

Once registered, any conversation in Claude.ai can use the boardgame tools. The `play-tictactoe` MCP prompt shows up in Claude's prompt picker.

## Try it locally first

The Dockerfile, the no-auth HTTP path, and the curl smoke test all work locally:

```sh
go build -o /tmp/boardgame-mcp ./mcp/cmd/boardgame-mcp
/tmp/boardgame-mcp serve --transport=http --port=8080 &

curl http://127.0.0.1:8080/healthz
# → ok

curl -X POST http://127.0.0.1:8080/mcp \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}'
```

Replace `127.0.0.1:8080` with your Cloud Run URL and add `-H "Authorization: Bearer <jwt>"` to test the OAuth path.

## Operator checklist

Before promoting a deployment:

- [ ] `--jwks-url`, `--issuer`, `--audience` all set.
- [ ] Cloud Run service is **not** publicly invokable without auth (rely on the connector's OAuth handshake; the binary's JWT verifier is the second layer).
- [ ] `/healthz` reachable from the platform's probe.
- [ ] Logs go to stderr (the binary's default — Cloud Run picks them up automatically).
- [ ] Billing alert set.

## Limitations (this PR)

- **`MemoryOwnership` in-memory.** Match ownership is lost on container restart. A Postgres-backed `OwnershipStore` is queued for a quick follow-up PR; until that lands, run with `minScale: 1` if persistence across instance shutdowns matters.
- **`MemoryStorage` default.** Match state itself is in-memory. Use `--db /tmp/state.sqlite` to persist (Cloud Run gives you a writable `/tmp`, gone on restart) or wait for the Postgres-everything PR.
- **No rate limiting** beyond Cloud Run's defaults. Add a layer once you have real usage.
- **No metrics endpoint** yet. Cloud Run's built-in request metrics cover the basics.

## Troubleshooting

**`401 missing bearer token`** — the request didn't include an `Authorization: Bearer <jwt>` header. If you're testing without OAuth, drop `--jwks-url` from the command line and the server accepts unauthenticated requests as `anonymous`.

**`401 invalid token: unsupported alg "HS256"`** — your issuer signed the JWT with HMAC. This is rejected by design. Configure the issuer to use RS256 or ES256.

**`401 invalid token: unknown kid "..." after refresh`** — the token's `kid` isn't in the JWKS document. Possible causes: wrong `--jwks-url`, recent key rotation that hasn't propagated, a token from a different issuer. The verifier rate-limits JWKS refreshes to once per minute, so consecutive bad tokens won't hammer the endpoint.

**Empty `games` in `list_games`** — at startup the binary registers only `tic-tac-toe`. Add games via `mgr.MustRegister` in `mcp/cmd/boardgame-mcp/main.go`.
