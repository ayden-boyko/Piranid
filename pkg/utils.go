package internal

import (
	"Piranid/node"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
)

// Shared helper functions
func NewServiceID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}

func SetUpDB(node *node.Node, dbType string, dbPath string, schemaPath string) {
	fmt.Println("Initializing database...")

	db, err := sql.Open(dbType, dbPath)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	fmt.Println("Database initialized...")

	node.SetDB(db)

	// Read the contents of the initfile
	sqlScript, err := os.ReadFile(schemaPath)
	if err != nil {
		log.Fatalf("Error reading SQL script: %v", err)
	}

	// Execute the SQL script to initialize the database
	db, ok := node.GetDB().(*sql.DB)
	if !ok {
		log.Fatalf("Error, expected server.Node.GetDB() to be of type *sql.DB, but got %T", node.GetDB())

	}
	_, err = db.Exec(string(sqlScript))
	if err != nil {
		log.Fatalf("Error executing SQL script: %v, error within %s", err, string(sqlScript))
	}
	fmt.Println("Query executed...")
}
