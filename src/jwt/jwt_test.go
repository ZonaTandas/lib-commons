package jwt_test

import (
	jwtLib "lib-commons/jwt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

func TestGenerateAndVerifyToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	input := jwtLib.Claims{
		UserId:      "user-uuid-123",
		TrackId:     "track-uuid-456",
		OrganizerId: "organizer-uuid-789",
		ActivityId:  "activity-uuid-000",
		IsAuth:      true,
	}

	tokenStr, err := jwtLib.GenerateToken(input)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	got, err := jwtLib.VerifyToken(tokenStr)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}

	if got.UserId != input.UserId {
		t.Errorf("UserId: esperado %q, obtenido %q", input.UserId, got.UserId)
	}
	if got.TrackId != input.TrackId {
		t.Errorf("TrackId: esperado %q, obtenido %q", input.TrackId, got.TrackId)
	}
	if got.OrganizerId != input.OrganizerId {
		t.Errorf("OrganizerId: esperado %q, obtenido %q", input.OrganizerId, got.OrganizerId)
	}
	if got.ActivityId != input.ActivityId {
		t.Errorf("ActivityId: esperado %q, obtenido %q", input.ActivityId, got.ActivityId)
	}
	if got.IsAuth != input.IsAuth {
		t.Errorf("IsAuth: esperado %v, obtenido %v", input.IsAuth, got.IsAuth)
	}
	if got.Exp == 0 {
		t.Error("Exp no debe ser 0")
	}
	if got.Iat == 0 {
		t.Error("Iat no debe ser 0")
	}
}

func TestExpiredToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	// Generamos un token expirado directamente con la librería subyacente
	expiredClaims := jwt.MapClaims{
		"userId":      "",
		"trackId":     "",
		"organizerId": "",
		"activityId":  "",
		"isAuth":      false,
		"exp":         jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		"iat":         jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	}
	raw := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims)
	expiredToken, err := raw.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("error generando token expirado: %v", err)
	}

	_, err = jwtLib.VerifyToken(expiredToken)
	if err != jwtLib.ErrExpiredToken {
		t.Errorf("se esperaba ErrExpiredToken, se obtuvo %v", err)
	}
}

func TestInvalidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	_, err := jwtLib.VerifyToken("esto.no.es.un.token.valido")
	if err != jwtLib.ErrInvalidToken {
		t.Errorf("se esperaba ErrInvalidToken, se obtuvo %v", err)
	}
}

func TestWrongSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	tokenStr, err := jwtLib.GenerateToken(jwtLib.Claims{UserId: "user-1", IsAuth: true})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Cambiar el secret y verificar que falla
	t.Setenv("JWT_SECRET", "wrong-secret")
	_, err = jwtLib.VerifyToken(tokenStr)
	if err != jwtLib.ErrInvalidToken {
		t.Errorf("se esperaba ErrInvalidToken, se obtuvo %v", err)
	}
}
