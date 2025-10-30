package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	data_manager "Piranid/pkg/DataManager"
	models "Piranid/pkg/models"

	transactions "github.com/ayden-boyko/Piranid/nodes/Auth/transactions"
)

func AuthTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func SignUpHandler(w http.ResponseWriter, r *http.Request, dm *data_manager.DataManagerImpl[models.Entry]) error {
	fmt.Println("Sign up received...")
	fmt.Fprint(w, "received")
	var req transactions.SignUpRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	return nil
}

// Handles requests for user authorization,
// showing consent screens and issuing authorization grants.

// user hits login page and login page redirects to auth server login page,
// once user enters info the auth code is sent to the client
func AuthHandler(w http.ResponseWriter, r *http.Request) error {
	var req transactions.ConsentPage
	var templatesFS embed.FS

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")

	tmpl, err := template.ParseFS(templatesFS, "templates/ConsentPage.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return errors.New("error parsing template")
	}
	tmpl.Execute(w, req)
	if err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return err
	}

	return nil
}

// Once the user signs in on the consent screen, the info is sent here where the auth server
// can verify the user information, if correct,
// the auth server responds to the client throught the callback (i.e redirect url) with the auth code
func LoginHandler(w http.ResponseWriter, r *http.Request) error {
	var req transactions.AuthRequest

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
	var req transactions.AuthExchange

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
