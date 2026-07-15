package obs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskJSON enmascara un cuerpo JSON antes de loguearlo:
//   - redacta por completo credenciales (password/token/secret/authorization/
//     apiKey/cardNumber/cvc/iban...) → "***"
//   - parcializa identificadores personales (dni/gobId/nif/nie...) → "12****8Z"
//   - un cuerpo que no sea JSON válido se omite entero (nunca se loguea en crudo:
//     podría llevar credenciales en form-data).
func MaskJSON(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return []byte(`"[cuerpo no-JSON omitido]"`)
	}
	masked := maskValue("", v)
	out, err := json.Marshal(masked)
	if err != nil {
		return []byte(`"[error al enmascarar]"`)
	}
	return out
}

// Claves que se redactan enteras si la clave normalizada las CONTIENE.
var redactContains = []string{"password", "token", "secret", "apikey", "authorization"}

// Claves que se redactan enteras por igualdad exacta (normalizada).
var redactExact = map[string]struct{}{
	"cardnumber": {}, "cvc": {}, "cvv": {}, "pan": {}, "iban": {},
	"pin": {}, "credentials": {},
}

// Claves que se parcializan (identificadores personales).
var partialExact = map[string]struct{}{
	"dni": {}, "gobid": {}, "nif": {}, "nie": {}, "govid": {},
	"documentnumber": {}, "passportnumber": {},
}

func normalizeKey(k string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
}

func maskValue(key string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			nk := normalizeKey(k)
			switch {
			case keyRedacted(nk):
				out[k] = "***"
			case keyPartial(nk):
				out[k] = partialize(val)
			default:
				out[k] = maskValue(k, val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskValue(key, val)
		}
		return out
	default:
		// El valor de una clave sensible que llega aquí dentro de un array
		// hereda la clave del padre:
		nk := normalizeKey(key)
		if keyRedacted(nk) {
			return "***"
		}
		if keyPartial(nk) {
			return partialize(v)
		}
		return v
	}
}

func keyRedacted(normalized string) bool {
	if _, ok := redactExact[normalized]; ok {
		return true
	}
	for _, s := range redactContains {
		if strings.Contains(normalized, s) {
			return true
		}
	}
	return false
}

func keyPartial(normalized string) bool {
	_, ok := partialExact[normalized]
	return ok
}

// partialize deja ver los 2 primeros y 2 últimos caracteres: "12****78Z" → "12*****8Z".
func partialize(v any) string {
	s := fmt.Sprint(v)
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}
