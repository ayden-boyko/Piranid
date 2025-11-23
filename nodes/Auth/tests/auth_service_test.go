package tests

import (
	"Piranid/node"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	utils "Piranid/pkg"
	data_manager "Piranid/pkg/DataManager"

	auth_utils "github.com/ayden-boyko/Piranid/nodes/Auth/utils"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	core "github.com/ayden-boyko/Piranid/nodes/Auth/authcore"

	handler "github.com/ayden-boyko/Piranid/nodes/Auth/handlers"
)

var server *core.AuthNode
var credentials_manager *data_manager.DataManagerImpl[model.AuthEntry]
var auth_code_manager *data_manager.DataManagerImpl[model.AuthCodeEntry]

//todo create test token

func init() {

	server = &core.AuthNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("ATST")}

	db, err := sql.Open("sqlite", "./Auth_DB.db")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	fmt.Println("Database initialized...")

	server.Node.SetDB(db)

	// Read the contents of the initfile
	sqlScript, err := os.ReadFile("./Schema.sql")
	if err != nil {
		log.Fatalf("Error reading SQL script: %v", err)
	}

	// Execute the SQL script to initialize the database
	db, ok := server.Node.GetDB().(*sql.DB)
	if !ok {
		log.Fatalf("Error, expected server.Node.GetDB() to be of type *sql.DB, but got %T", server.Node.GetDB())
	}
	_, err = db.Exec(string(sqlScript))
	if err != nil {
		log.Fatalf("Error executing SQL script: %v, error within %s", err, string(sqlScript))
	}
	fmt.Println("Query executed...")

	credentials_manager, err = data_manager.NewDataManager[model.AuthEntry](db, "credentials")
	if err != nil {
		log.Fatalf("Error creating manager: %v", err)
	}

	auth_code_manager, err = data_manager.NewDataManager[model.AuthCodeEntry](db, "auth_codes")
	if err != nil {
		log.Fatalf("Error creating manager: %v", err)
	}

	fmt.Println("Auth Node created...")
	fmt.Println("Initializing database...")

}

func TestSignUpAndLogin(t *testing.T) {
	// Construct test server and DB setup (assumed done in init())

	// Prepare signup request payload
	signUpPayload := map[string]string{
		"Username":       "testuser",
		"HashedPassword": "hashedpass",
		"Useremail":      "test@example.com",
		"ClientSecret":   "secret",
		"ClientId":       "clientid123",
		"ServiceId":      server.Service_ID,
		"RedirectURI":    "http://localhost/callback",
	}
	payloadBytes, _ := json.Marshal(signUpPayload)

	// Perform SignUpHandler test
	req := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewReader(payloadBytes))
	w := httptest.NewRecorder()
	err := handler.SignUpHandler(w, req, credentials_manager)
	if err != nil {
		t.Fatalf("SignUpHandler failed: %v", err)
	}
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Result().StatusCode)
	}

	// Prepare login request payload
	loginPayload := map[string]string{
		"Username":    "testuser",
		"RedirectURI": "http://localhost/callback",
	}
	loginBytes, _ := json.Marshal(loginPayload)

	// Perform LoginHandler test (which generates auth code in DB)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(loginBytes))
	loginW := httptest.NewRecorder()
	err = handler.LoginHandler(loginW, loginReq, credentials_manager, auth_code_manager)
	if err != nil {
		t.Fatalf("LoginHandler failed: %v", err)
	}
	if loginW.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status OK, got %d", loginW.Result().StatusCode)
	}

	// Exchange auth code for token
	tokenReq := httptest.NewRequest(http.MethodPost, "/token", bytes.NewReader(loginBytes))
	tokenW := httptest.NewRecorder()
	err = handler.TokenHandler(tokenW, tokenReq, credentials_manager, auth_code_manager)
	if err != nil {
		t.Fatalf("TokenHandler failed: %v", err)
	}
	if tokenW.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected status OK, got %d", tokenW.Result().StatusCode)
	}

	// Decode and validate returned auth code token
	var token string
	err = json.NewDecoder(loginW.Body).Decode(&token)
	if err != nil {
		t.Fatalf("decoding token failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected token, got empty string")
	}

	// You can now use 'token' in further tests for TokenHandler or UserInfoHandler

	// Optionally, verify auth code persistence in DB directly:
	authCodeEntry, err := auth_code_manager.GetEntry("authcode", token, auth_utils.AuthCodeScanner)
	if err != nil || authCodeEntry.AuthCode != token {
		t.Fatalf("expected auth code entry in DB, got err=%v", err)
	}
}

func TestLogout(t *testing.T) {

}

func TestUserInfo(t *testing.T) {

}
