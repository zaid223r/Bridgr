package bridgr_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	bridgr "github.com/zaid223r/Bridgr"
)

type Login struct {
	ID       int64  `json:"id"`
	User     string `json:"user"`
	Password string `json:"password,omitzero" bridgr:"writeonly"`
}

func TestHandleHidesWriteOnlyFields(t *testing.T) {
	t.Parallel()
	shared := Login{ID: 1, User: "zaid", Password: "secret"}
	list := []Login{shared, shared}
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	bridgr.Handle(app, "GET", "/value", func(context.Context, struct{}) (Login, error) { return shared, nil })
	bridgr.Handle(app, "GET", "/pointer", func(context.Context, struct{}) (*Login, error) { return &shared, nil })
	bridgr.Handle(app, "GET", "/nil", func(context.Context, struct{}) (*Login, error) { return nil, nil })
	bridgr.Handle(app, "GET", "/list", func(context.Context, struct{}) ([]Login, error) { return list, nil })
	h := app.Handler()

	for path, want := range map[string]string{
		"/value":   `{"id":1,"user":"zaid"}`,
		"/pointer": `{"id":1,"user":"zaid"}`,
		"/nil":     `null`,
		"/list":    `[{"id":1,"user":"zaid"},{"id":1,"user":"zaid"}]`,
	} {
		rec := do(t, h, "GET", path, "")
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
			t.Errorf("GET %s = %d %s, want %s", path, rec.Code, rec.Body, want)
		}
	}
	if shared.Password != "secret" || list[1].Password != "secret" {
		t.Error("hiding changed the handler's own values")
	}
}

func TestHandleRejectsNestedWriteOnly(t *testing.T) {
	t.Parallel()
	type session struct {
		Login Login `json:"login"`
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "session.Login has writeonly fields") {
			t.Errorf("recover() = %v, want a panic naming the nested field", r)
		}
	}()
	bridgr.Handle(bridgr.New(bridgr.Config{}), "GET", "/session", func(context.Context, struct{}) (session, error) {
		return session{}, nil
	})
}
