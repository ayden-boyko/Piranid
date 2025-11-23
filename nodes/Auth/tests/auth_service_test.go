package tests

import (
	"Piranid/node"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/go-redis/redis"
	_ "modernc.org/sqlite"

	utils "Piranid/pkg"
)

type TestAuthNode struct {
	*node.Node
	Service_ID string
	Cache      *redis.Client
}

func init() {

	server := &TestAuthNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("AUTH")}

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

}
