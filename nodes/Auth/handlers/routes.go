package handlers

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	utils "github.com/ayden-boyko/Piranid/nodes/Auth/utils"
	"go.uber.org/zap"

	data_manager "Piranid/pkg/DataManager"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	transactions "github.com/ayden-boyko/Piranid/nodes/Auth/transactions"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("auth/handlers")

func AuthTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "received")
}

// TODO: ADD PKCE, https://medium.com/@dipakkrdas/pkce-explained-securing-oauth-without-the-secrets-bbaf83f04959

func SignUpHandler(w http.ResponseWriter, r *http.Request, dm *data_manager.DataManagerImpl[model.AuthEntry], ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "SignUpHandler")
	defer span.End()

	logger.Info("Sign up received...")
	var req transactions.SignUpRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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

	logger.Info(fmt.Sprintf("User %s signed up successfully", req.Username))
	span.SetStatus(codes.Ok, "")
	w.WriteHeader(http.StatusOK)

	return nil
}

// handles requests for user sign up,
// showing sign up page
func SignUpPageHandler(w http.ResponseWriter, r *http.Request, templatesFS embed.FS, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "SignUpPageHandler")
	defer span.End()

	logger.Info("Sign up received...")
	var req transactions.SignUpPage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}

	tmpl, err := template.ParseFS(templatesFS, "templates/SignUpPage.html")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		msg := "error parsing template"
		logger.Error(msg)
		return errors.New(msg)
	}
	err = tmpl.Execute(w, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := "error executing template"
		logger.Error(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return err
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("Sign up page rendered successfully")
	return nil
}

// Handles requests for user authorization,
// showing consent screens and issuing authorization grants.

// user hits login page and login page redirects to auth server login page,
// once user enters info the auth code is sent to the client
func AuthPageHandler(w http.ResponseWriter, r *http.Request, templatesFS embed.FS, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "AuthPageHandler")
	defer span.End()

	var req transactions.ConsentPage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}
	logger.Info("Auth received...")

	tmpl, err := template.ParseFS(templatesFS, "templates/ConsentPage.html")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		msg := "error parsing template"
		logger.Error(msg)
		return errors.New(msg)
	}
	err = tmpl.Execute(w, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := "error executing template"
		logger.Error(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return err
	}

	span.SetStatus(codes.Ok, "")
	logger.Info("Auth page rendered successfully")
	return nil
}

// Once the user signs in on the consent screen, the info is sent here where the auth server
// can verify the user information, if correct,
// the auth server responds to the client throught the callback (i.e redirect url) with the auth code
func LoginHandler(w http.ResponseWriter, r *http.Request, ae *data_manager.DataManagerImpl[model.AuthEntry], ace *data_manager.DataManagerImpl[model.AuthCodeEntry], ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "LoginHandler")
	defer span.End()

	var req transactions.AuthRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}

	// check if the user exists in the database
	entry, err := ae.GetEntry("username", req.Username, utils.CredentialsScanner)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}

	if entry == (model.AuthEntry{}) { // entry is empty, user doesnt exist
		msg := fmt.Sprintf("User %s does not exist", req.Username)
		logger.Error(msg)
		span.RecordError(errors.New("user does not exist"))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		return errors.New(msg)
	}

	//check if redirect url is valid
	if entry.RedirectURI != req.RedirectURI {
		msg := "Invalid redirect url"
		span.RecordError(errors.New(msg))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		return errors.New(msg)
	}

	logger.Info("Login received...")
	// create auth code JWT
	token, time, err := utils.CreateToken(entry.ClientId)
	if err != nil {
		msg := "Error creating token"
		logger.Error(fmt.Sprintf("%s: %v", msg, err))
		span.RecordError(err)
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return errors.New(msg)
	}

	// add auth code to database
	if err := ace.PushData(model.AuthCodeEntry{AuthCode: token, Expires: time}, utils.AuthCodeInserter); err != nil {
		msg := "Error adding auth code to database"
		logger.Error(fmt.Sprintf("%s: %v", msg, err))
		span.RecordError(err)
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return errors.New(msg)
	}

	// return auth code
	span.SetStatus(codes.Ok, "")
	w.Header().Set("Content-Type", "application/json")
	logger.Info("Auth code generated", zap.String("auth_code", token))
	json.NewEncoder(w).Encode(token)

	return nil
}

// once the client gets auth code,
// it makes a call to the auth server to exchange the code for an access token
func TokenHandler(w http.ResponseWriter, r *http.Request, ae *data_manager.DataManagerImpl[model.AuthEntry], ace *data_manager.DataManagerImpl[model.AuthCodeEntry], ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "TokenHandler")
	defer span.End()

	var req transactions.AuthExchange

	var entry model.AuthEntry

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}
	logger.Info("Token received...")

	// check if auth code is valid
	auth_code, err := ace.GetEntry("authcode", req.Authcode, utils.AuthCodeScanner)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "error decoding request body"
		logger.Error(msg)
		return errors.New(msg)
	}

	if auth_code == (model.AuthCodeEntry{}) { // entry is empty, auth code doesnt exist
		msg := "Invalid auth code"
		span.RecordError(errors.New(msg))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		logger.Error(msg)
		return errors.New(msg)
	}
	// check if auth code is expired
	if auth_code.Expires < time.Now().Unix() {
		msg := "Auth code expired"
		span.RecordError(errors.New(msg))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		logger.Error(msg)
		return errors.New(msg)
	}

	// at this point, auth code is valid
	// remove auth code from database
	if err := ace.DeleteData(model.AuthCodeEntry{AuthCode: req.Authcode}, utils.AuthCodeDeleter); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		msg := "Error removing auth code from database"
		logger.Error(fmt.Sprintf("%s: %v", msg, err))
		return errors.New(msg)
	}

	// get user auth_entry
	entry, err = ae.GetEntry("clientid", req.ClientId, utils.CredentialsScanner)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		msg := "Error fetching user credentials"
		logger.Error(fmt.Sprintf("%s: %v", msg, err))
		return errors.New(msg)
	}

	// check if redirect url is valid
	if entry.RedirectURI != req.RedirectURI {
		msg := "Invalid redirect url"
		span.RecordError(errors.New(msg))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		logger.Error(msg)
		return errors.New(msg)
	}

	// check if client secret in db matches based on client id
	if entry.ClientSecret != req.ClientSecret {
		msg := "Invalid client secret"
		span.RecordError(errors.New(msg))
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, http.StatusBadRequest)
		logger.Error(msg)
		return errors.New(msg)
	}

	// if auth code and client secret valid
	var res transactions.AuthToken

	// TODO, create dedicated auth & refresh token methods
	// return access token JWT
	res.AccessToken, _, _ = utils.CreateToken(entry.ClientId)
	res.RefreshToken, _, _ = utils.CreateToken(entry.ClientId)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)

	span.SetStatus(codes.Ok, "")
	logger.Info("Access token generated", zap.String("access_token", res.AccessToken), zap.String("refresh_token", res.RefreshToken))
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
