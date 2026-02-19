//go:build ignore

// genpw generates a bcrypt hash for a plaintext password, suitable for
// inserting directly into the users.password_hash column.
//
// Usage:
//
//	go run scripts/genpw.go
//	go run scripts/genpw.go mypassword

package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	var password string
	if len(os.Args) > 1 {
		password = os.Args[1]
	} else {
		fmt.Fprint(os.Stderr, "Password: ")
		fmt.Scanln(&password)
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, "error: empty password")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
