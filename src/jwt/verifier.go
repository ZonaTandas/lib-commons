package jwt

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("token inválido o malformado")
	ErrExpiredToken = errors.New("token expirado")
)

func VerifyToken(tokenString string) (Claims, error) {
	jwtSecret := os.Getenv("JWT_SECRET")

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de firma inesperado: %v", t.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, ErrExpiredToken
		}
		return Claims{}, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return Claims{}, ErrInvalidToken
	}

	result := Claims{
		UserId:      claims["userId"].(string),
		TrackId:     claims["trackId"].(string),
		OrganizerId: claims["organizerId"].(string),
		ActivityId:  claims["activityId"].(string),
		IsAuth:      claims["isAuth"].(bool),
		Exp:         int64(claims["exp"].(float64)),
		Iat:         int64(claims["iat"].(float64)),
	}

	return result, nil
}
