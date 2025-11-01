package utils

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var secret_key = []byte(os.Getenv("JWT_SECRET"))

func CreateToken(client_id string) (string, int64, error) {
	expire_time := time.Now().Add(time.Hour * 24).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{
			"client_id": client_id,
			"iat":       time.Now().Unix(),
			"exp":       expire_time})
	tokenString, err := token.SignedString(secret_key)
	if err != nil {
		return "", 0, err
	}
	return tokenString, expire_time, nil
}

func VerifyToken(tokenString string) error {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return secret_key, nil
	})

	if err != nil {
		return err
	}

	if !token.Valid {
		return fmt.Errorf("invalid token")
	}

	return nil
}
