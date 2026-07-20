// internal-token emite un token caducable de X-Internal-Auth (evolutivo
// 2026-07-red-interna-auth) para acceder a los servicios desde fuera del
// cluster (Postman, curl) sin repartir el secreto en claro:
//
//	INTERNAL_SHARED_SECRET=... go run ./cmd/internal-token -subject dev-alejandro -ttl 2h
//
// Imprime el valor a poner en la cabecera X-Internal-Auth. Cualquier servicio
// lo valida offline (HMAC-SHA256 con el mismo secreto + expiración).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ZonaTandas/lib-commons/src/obs"
)

func main() {
	subject := flag.String("subject", "", "identificador del portador (sale en logs/métricas), p. ej. dev-alejandro")
	ttl := flag.Duration("ttl", 2*time.Hour, "validez del token (p. ej. 30m, 2h, 24h)")
	flag.Parse()

	if *subject == "" {
		fmt.Fprintln(os.Stderr, "uso: INTERNAL_SHARED_SECRET=... internal-token -subject dev-<nombre> [-ttl 2h]")
		os.Exit(2)
	}
	token, err := obs.NewInternalAuthToken(*subject, *ttl)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(token)
}
