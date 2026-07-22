package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ZonaTandas/lib-commons/src/obs"
)

// Cubre el camino feliz del CLI in-process (los caminos de error hacen
// os.Exit y quedan fuera de cobertura; ver notas del evolutivo).
func TestMainEmiteTokenValido(t *testing.T) {
	t.Setenv("INTERNAL_SHARED_SECRET", "sec")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"internal-token", "-subject", "dev-test", "-ttl", "30m"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	main()
	w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(out))
	subject, result := obs.ValidateInternalAuth("sec", token)
	if result != "ok_token" || subject != "dev-test" {
		t.Errorf("token emitido no valida: result=%q subject=%q (token %q)", result, subject, token)
	}
}
