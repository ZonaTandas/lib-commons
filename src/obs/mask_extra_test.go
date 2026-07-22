package obs

import (
	"encoding/json"
	"testing"
)

func TestMaskJSONParcialesCortosYAnidados(t *testing.T) {
	out := MaskJSON([]byte(`{"dni":"12Z","items":[{"nif":"12345678Z"}],"password":123}`))
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v["dni"] != "****" {
		t.Errorf("dni corto = %v, esperaba ****", v["dni"])
	}
	item := v["items"].([]any)[0].(map[string]any)
	if item["nif"] != "12*****8Z" {
		t.Errorf("nif = %v, esperaba 12*****8Z", item["nif"])
	}
	if v["password"] != "***" {
		t.Errorf("password numérica = %v, esperaba ***", v["password"])
	}
}

func TestMaskJSONArrayYEscalarTopLevel(t *testing.T) {
	if got := string(MaskJSON([]byte(`["a",1]`))); got != `["a",1]` {
		t.Errorf("array top-level = %s", got)
	}
	if got := string(MaskJSON([]byte(`"hola"`))); got != `"hola"` {
		t.Errorf("escalar top-level = %s", got)
	}
}

func TestMaskJSONEnterosGrandes(t *testing.T) {
	// FIXME libcommons-03: MaskJSON pasa por float64 (json.Unmarshal a any) y
	// corrompe enteros de más de 2^53 en los logs. El test afirma el
	// comportamiento esperado (el número sale intacto); se salta hasta que se
	// corrija el hallazgo.
	t.Skip("known-failure libcommons-03: enteros >2^53 se corrompen al re-serializar")
	got := string(MaskJSON([]byte(`{"id":1234567890123456789}`)))
	if got != `{"id":1234567890123456789}` {
		t.Errorf("entero grande corrompido: %s", got)
	}
}
