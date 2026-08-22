package httpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler() retornou erro: %v", err)
	}
	return handler
}

func TestHealth(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	testHandler(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("resposta JSON inválida: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status do corpo = %q, esperado ok", body["status"])
	}
}

func TestMainRoutes(t *testing.T) {
	routes := []struct {
		path string
		text string
	}{
		{path: "/", text: "Crie hábitos melhores"},
		{path: "/criar-habito", text: "Criar Hábito"},
		{path: "/meus-habitos", text: "Meus Hábitos"},
		{path: "/progresso", text: "Progresso"},
		{path: "/recompensas", text: "Recompensas"},
		{path: "/aprenda-4rs", text: "Aprenda os 4 Rs"},
		{path: "/perfil", text: "Perfil"},
	}

	handler := testHandler(t)
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, route.path, nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
			}
			if !strings.Contains(recorder.Body.String(), route.text) {
				t.Fatalf("resposta não contém %q", route.text)
			}
		})
	}
}

func TestStaticAssetAndServiceWorker(t *testing.T) {
	tests := []struct {
		path        string
		contentType string
	}{
		{path: "/static/css/app.css", contentType: "text/css"},
		{path: "/static/js/app.js", contentType: "text/javascript"},
		{path: "/static/manifest.webmanifest", contentType: "application/manifest+json"},
		{path: "/service-worker.js", contentType: "text/javascript"},
	}

	handler := testHandler(t)
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusOK)
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, test.contentType) {
				t.Fatalf("Content-Type = %q, esperado conter %q", contentType, test.contentType)
			}
		})
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/nao-existe", nil)

	testHandler(t).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado %d", recorder.Code, http.StatusNotFound)
	}
}
