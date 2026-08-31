package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/robertobado/netsk8-navigator/backend/internal/api"
)

// App holds the Wails runtime context — no JS-bound methods needed, since
// the frontend already talks to the backend over its existing REST/SSE/WS
// API rather than a Wails JS bridge (which, per main.go's bootstrapRedirect
// comment, is never actually present in this app's window). It exists so
// the "Open in Browser" menu action (main.go) has a ctx to call
// runtime.BrowserOpenURL with, and so startup can wire srv's native
// "open externally" hook once that ctx exists.
type App struct {
	ctx context.Context
	srv *api.Server
}

func NewApp(srv *api.Server) *App {
	return &App{srv: srv}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// See externalopen.go: the frontend's openExternal() POSTs here instead
	// of calling a JS bridge that doesn't exist in this app.
	a.srv.SetExternalOpener(func(url string) {
		runtime.BrowserOpenURL(ctx, url)
	})
}
