package adminui

import (
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestEmbeddedLocalizedAssets(t *testing.T) {
	for _, path := range []string{"/admin/", "/admin/app.js", "/admin/i18n.js", "/admin/app.css", "/admin/favicon.svg"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatalf("missing local-script policy: %s", path)
		}
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/admin/i18n.test.cjs", nil))
	if w.Code != 404 {
		t.Fatal("test source must not be embedded")
	}
}
func TestLocaleCatalogAndDetection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is needed only for JavaScript authoring tests")
	}
	command := exec.Command(node, "--test", "i18n.test.cjs")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("i18n behavior tests: %v\n%s", err, output)
	}
}
