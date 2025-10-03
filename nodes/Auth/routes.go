package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

func AuthTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func SignUpHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Sign up received...")
	fmt.Fprint(w, "received")
}

func SignInHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Sign in received...")
	fmt.Fprint(w, "received")

	// TODO, isnt password supposed to be hashed?
	request := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{}

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email":    request.Email,
		"password": request.Password,
	})
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"token": "` + tokenString + `"}`))
}

func SignOutHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Sign out received...")
	fmt.Fprint(w, "received")
}

func ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Change password received...")
	fmt.Fprint(w, "received")
}

func ChangeUserNameHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Change email received...")
	fmt.Fprint(w, "received")
}

func ChangeEmailHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Change email received...")
	fmt.Fprint(w, "received")
}

func DeleteAccountHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Delete account received...")
	fmt.Fprint(w, "received")
}
