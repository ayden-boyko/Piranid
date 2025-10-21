package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	models "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

func AuthTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

// Handles requests for user authorization,
// showing consent screens and issuing authorization grants.

// user hits login page and login page redirects to auth server login page,
// this handler returns the info for the user agent (new page with consent screen)
func AuthHandler(w http.ResponseWriter, r *http.Request) error {
	var req models.ConsentPage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")

	return nil
}

// Once the user signs in on the consent screen, the info is sent here where the auth server
// can verify the user information, if correct,
// the auth server responds to the client throught the callback (i.e redirect url) with the auth code
func LoginHandler(w http.ResponseWriter, r *http.Request) error {
	var req models.AuthRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}

	fmt.Println("Login received...")
	fmt.Fprint(w, "received")

	return nil
}

// once the client gets auth code,
// it makes a call to the auth server to exchange the code for an access token
func TokenHandler(w http.ResponseWriter, r *http.Request) error {
	var req models.AuthExchange

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}
	fmt.Println("Token received...")
	fmt.Fprint(w, "received")
	return nil
}

// Allows a client to obtain profile info for a user with a valid access token.
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("User info received...")
	fmt.Fprint(w, "received")
}

// Handles centralized log out for sessions.
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Logout received...")
	fmt.Fprint(w, "received")
}
