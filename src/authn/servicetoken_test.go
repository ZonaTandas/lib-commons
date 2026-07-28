package authn

import (
	"net/http"
	"testing"
)

// El bug de notif-02: con el env vacío, una petición SIN cabecera pasaba
// porque "" == "". Este test falla contra aquella implementación.
func TestCheckServiceTokenSinConfigurarRechaza(t *testing.T) {
	t.Setenv("AUTH_SERVICE_TOKEN", "")
	ok, configured := CheckServiceToken("")
	if ok {
		t.Error("con el secreto sin configurar NO debe pasar (fail-open de notif-02)")
	}
	if configured {
		t.Error("configured debe ser false para poder responder 503 y no 401")
	}
}

func TestCheckServiceToken(t *testing.T) {
	t.Setenv("AUTH_SERVICE_TOKEN", "esperado")
	cases := []struct {
		provided string
		want     bool
	}{
		{"esperado", true},
		{"  esperado  ", true},
		{"esperadX", false}, // misma longitud
		{"esperado-largo", false},
		{"", false},
	}
	for _, tc := range cases {
		ok, configured := CheckServiceToken(tc.provided)
		if !configured {
			t.Fatal("configured debería ser true")
		}
		if ok != tc.want {
			t.Errorf("CheckServiceToken(%q) = %v, want %v", tc.provided, ok, tc.want)
		}
	}
}

// El env se lee POR REQUEST: un secreto montado después del arranque debe
// empezar a funcionar sin reiniciar el proceso.
func TestCheckServiceTokenLeeElEnvEnCadaLlamada(t *testing.T) {
	t.Setenv("AUTH_SERVICE_TOKEN", "")
	if ok, _ := CheckServiceToken("nuevo"); ok {
		t.Fatal("aún no configurado")
	}
	t.Setenv("AUTH_SERVICE_TOKEN", "nuevo")
	if ok, configured := CheckServiceToken("nuevo"); !ok || !configured {
		t.Error("tras montar el secreto debe aceptar sin reiniciar")
	}
}

func TestCheckServiceTokenRequest(t *testing.T) {
	t.Setenv("AUTH_SERVICE_TOKEN", "tok")
	r, _ := http.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer tok")
	if ok, _ := CheckServiceTokenRequest(r); !ok {
		t.Error("debe aceptar el token con prefijo Bearer")
	}
	r.Header.Set("Authorization", "tok")
	if ok, _ := CheckServiceTokenRequest(r); !ok {
		t.Error("debe aceptar el token sin prefijo Bearer")
	}
	r.Header.Del("Authorization")
	if ok, _ := CheckServiceTokenRequest(r); ok {
		t.Error("sin cabecera debe rechazar")
	}
}

func TestCheckServiceTokenAgainst(t *testing.T) {
	if ok, configured := CheckServiceTokenAgainst("a", ""); ok || configured {
		t.Error("expected vacío ⇒ (false,false)")
	}
	if ok, configured := CheckServiceTokenAgainst("a", "a"); !ok || !configured {
		t.Error("expected == provided ⇒ (true,true)")
	}
}
