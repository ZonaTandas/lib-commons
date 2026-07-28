package authn

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// CheckServiceToken valida el AUTH_SERVICE_TOKEN que traen las llamadas
// service-to-service.
//
// Tres propiedades que las copias por servicio no siempre tenían:
//
//  1. **Fail-closed si no está configurado.** notif-02 (evolutivo de
//     saneamiento) capturaba el env en una `var` de paquete: si el secreto no
//     estaba montado al arrancar, `"" == ""` dejaba pasar peticiones SIN
//     cabecera Authorization. Aquí un secreto vacío rechaza siempre.
//  2. **Lectura por request.** El env se lee en cada llamada, así que un
//     secreto montado después del arranque no deja el proceso en un estado
//     permanentemente roto.
//  3. **Comparación en tiempo constante** (subtle.ConstantTimeCompare). No es
//     explotable en la práctica sobre un token aleatorio a través de la red,
//     pero es gratis y quita la familia PC-11 del inventario.
//
// Devuelve (ok, configured): !configured ⇒ 503 (misconfig nuestra),
// configured && !ok ⇒ 401.
func CheckServiceToken(provided string) (ok bool, configured bool) {
	return CheckServiceTokenAgainst(provided, os.Getenv("AUTH_SERVICE_TOKEN"))
}

// CheckServiceTokenAgainst es CheckServiceToken contra un valor esperado
// explícito (tests, o servicios con más de un secreto).
func CheckServiceTokenAgainst(provided, expected string) (ok bool, configured bool) {
	if expected == "" {
		return false, false
	}
	provided = strings.TrimSpace(provided)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1, true
}

// CheckServiceTokenRequest es CheckServiceToken leyendo la cabecera
// Authorization de la petición (con o sin prefijo "Bearer ").
func CheckServiceTokenRequest(r *http.Request) (ok bool, configured bool) {
	return CheckServiceToken(BearerToken(r.Header.Get("Authorization")))
}
