package main

import "context"

// App holds the Wails runtime context — no JS-bound methods needed, since
// the frontend already talks to the backend over its existing REST/SSE/WS
// API rather than a Wails JS bridge. It exists only so the "Open in Browser"
// menu action (main.go) has a ctx to call runtime.BrowserOpenURL with.
type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}
