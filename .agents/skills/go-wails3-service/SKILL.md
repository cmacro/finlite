---
name: go-wails3-service
description: Build Go Wails3 backend Services and call them from Svelte 5 frontend. Use when adding a Service method, registering services, fixing bindings, or wiring a page to the backend.
---

# Go Wails3 Service

finlite stack: Go 1.25 + Wails3 `v3.0.0-beta` + Svelte 5 + TS. Reference: `internal/app/app.go`, `internal/services/greet.go`, `frontend/src/App.svelte`.

## Service signature

- One struct per domain in `internal/services/*.go` (see AGENTS.md table).
- Blocking/DB work: first param `context.Context`.
- Always return `(T, error)`. Error text is user-facing Simplified Chinese.
- Never `panic` in Services; never `log.Fatal` outside `main.go`.

```go
package services

import "context"

type VoucherService struct {
    // store dependency injected from app.go, not created inline
}

type CreateVoucherReq struct {
    SummaryID string `json:"summaryId"`
}

func (s *VoucherService) CreateVoucher(ctx context.Context, req CreateVoucherReq) (Voucher, error) {
    // validate -> store txn -> audit log -> return
}
```

## Registration

Register every Service in `internal/app/app.go`:

```go
app := application.New(application.Options{
    Services: []application.Service{
        application.NewService(&services.GreetService{}),
        application.NewService(&services.VoucherService{ /* store */ }),
    },
})
```

`main.go` stays thin: embed `frontend/dist` + call `app.Run(assets)`.

## Frontend calls

- Bindings in `frontend/bindings/` are GENERATED. Never hand-edit; rebuild via Wails binding generation.
- Svelte 5 runes only (`$state`, `$props`, `$derived`), TS strict.
- Pattern (see `frontend/src/App.svelte`):

```ts
import { VoucherService } from "../bindings/finlite/internal/services";

let vouchers = $state<Voucher[]>([]);
VoucherService.ListVouchers({ status: "draft" }).then(v => vouchers = v).catch(console.error);
```

- No business math in components (formatting only). No direct SQLite access.
- Confirm dialog required for: balance confirm, audit/reverse-audit, restore backup.

## Checklist

- Method on struct pointer receiver, exported, JSON-tagged req/resp?
- Registered in `app.go`?
- Error message Chinese + actionable?
- Frontend imports from `bindings`, not hand-rolled fetch?
- `gofmt -l ./...`, `go vet ./...`, `go build ./...`, `npm run check --prefix frontend` pass?
