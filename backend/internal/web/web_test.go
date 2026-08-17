package web

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSpaHandler_NoIndexReturnsNil(t *testing.T) {
	empty := fstest.MapFS{"dist/.gitkeep": &fstest.MapFile{}}
	if spaHandler(empty) != nil {
		t.Error("expected nil handler when dist/ has no index.html (unbuilt frontend)")
	}
}

func TestSpaHandler_ServesRealFilesAndFallsBackForRoutes(t *testing.T) {
	fsys := fstest.MapFS{
		"dist/index.html":    &fstest.MapFile{Data: []byte("<html>shell</html>")},
		"dist/assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"dist/favicon.ico":   &fstest.MapFile{Data: []byte("icon")},
	}
	h := spaHandler(fsys)
	if h == nil {
		t.Fatal("expected a non-nil handler when index.html is present")
	}

	t.Run("real asset served as-is", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/app.js", nil))
		if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
			t.Errorf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("client-side route falls back to index.html", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/some/deep/route", nil))
		if rec.Code != 200 || rec.Body.String() != "<html>shell</html>" {
			t.Errorf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("root serves index.html", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != 200 || rec.Body.String() != "<html>shell</html>" {
			t.Errorf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})
}
