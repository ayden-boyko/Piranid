package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	utils "github.com/ayden-boyko/Piranid/nodes/Auth/utils"

	data_manager "Piranid/pkg/DataManager"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	transactions "github.com/ayden-boyko/Piranid/nodes/Auth/transactions"
)

func AuthTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func SignUpHandler(w http.ResponseWriter, r *http.Request, dm *data_manager.DataManagerImpl[model.AuthEntry]) error {
	fmt.Println("Sign up received...")
	fmt.Fprint(w, "received")
	var req transactions.SignUpRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if err := dm.PushData(
		model.AuthEntry{Username: req.Username,
			HashedPassword: req.HashedPassword,
			UserEmail:      req.Useremail,
			ClientSecret:   req.ClientSecret,
			ClientId:       req.ClientId,
			ServiceId:      req.ServiceId,
			RedirectURI:    req.RedirectURI}, utils.CredentialsInserter); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}

	w.WriteHeader(http.StatusOK)

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
	err = tmpl.Execute(w, req)
	if err != nil {
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return err
	}

	return nil
}

// Once the user signs in on the consent screen, the info is sent here where the auth server
// can verify the user information, if correct,
// the auth server responds to the client throught the callback (i.e redirect url) with the auth code
func LoginHandler(w http.ResponseWriter, r *http.Request, ae *data_manager.DataManagerImpl[model.AuthEntry], ace *data_manager.DataManagerImpl[model.AuthCodeEntry]) error {
	var req transactions.AuthRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}

	// check if the user exists in the database
	entry, err := ae.GetEntry("username", req.Username, utils.CredentialsScanner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return errors.New("error decoding request body")
	}

	if entry == (model.AuthEntry{}) { // entry is empty, user doesnt exist
		http.Error(w, "User does not exist", http.StatusBadRequest)
		return errors.New("user does not exist")
	}

	//check if redirect url is valid
	if entry.RedirectURI != req.RedirectURI {
		http.Error(w, "Invalid redirect url", http.StatusBadRequest)
		return errors.New("invalid redirect url")
	}

	fmt.Println("Login received...")
	fmt.Fprint(w, "received")

	// create auth code JWT
	token, time, err := utils.CreateToken(entry.ClientId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return errors.New("error creating token")
	}

	// add auth code to database
	if err := ace.PushData(model.AuthCodeEntry{AuthCode: token, Expires: time}, utils.AuthCodeInserter); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return errors.New("error adding auth code to database")
	}

	// return auth code
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(token)

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

	// check if auth code is valid

	// check if redirect url is valid

	// check if client secret in db matches based on client id

	// if auth code and client secret valid

	// return access token JWT

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
