package jwt

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func GenerateToken(claims Claims) (string, error) {
	jwtClaims := jwt.MapClaims{}

	jwtClaims["userId"] = claims.UserId
	jwtClaims["trackId"] = claims.TrackId
	jwtClaims["organizerId"] = claims.OrganizerId
	jwtClaims["activityId"] = claims.ActivityId
	jwtClaims["isAuth"] = claims.IsAuth

	jwtClaims["exp"] = jwt.NewNumericDate(time.Now().Add(1 * time.Hour))
	jwtClaims["iat"] = jwt.NewNumericDate(time.Now())

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	jwtSecret := os.Getenv("JWT_SECRET")
	return token.SignedString([]byte(jwtSecret))
}
