package obs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskJSONRedactsCredentials(t *testing.T) {
	in := `{
		"password": "hunter2",
		"accessToken": "jwt",
		"client_secret": "s3cr3t",
		"Authorization": "Bearer xyz",
		"apiKey": "k",
		"cardNumber": "4242424242424242",
		"cvc": "123",
		"iban": "ES9121000418450200051332",
		"nested": {"refresh_token": "r"},
		"list": [{"password": "p2"}],
		"normal": "visible"
	}`
	out := string(MaskJSON([]byte(in)))
	for _, leaked := range []string{"hunter2", "jwt", "s3cr3t", "Bearer xyz", "4242424242424242", "ES9121000418450200051332"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("fuga de %q en %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"normal":"visible"`) {
		t.Fatalf("campo normal perdido: %s", out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("la salida debe seguir siendo JSON: %v", err)
	}
	if m["password"] != "***" {
		t.Fatalf("password = %v", m["password"])
	}
}

func TestMaskJSONPartializesIDs(t *testing.T) {
	out := string(MaskJSON([]byte(`{"dni":"12345678Z","gobId":"X1234567L"}`)))
	if strings.Contains(out, "12345678Z") || strings.Contains(out, "X1234567L") {
		t.Fatalf("identificador sin parcializar: %s", out)
	}
	if !strings.Contains(out, `"dni":"12*****8Z"`) {
		t.Fatalf("parcializado inesperado: %s", out)
	}
}

func TestMaskJSONNonJSON(t *testing.T) {
	out := string(MaskJSON([]byte("user=ana&password=hunter2")))
	if strings.Contains(out, "hunter2") {
		t.Fatalf("un cuerpo no-JSON jamás se loguea en crudo: %s", out)
	}
}
