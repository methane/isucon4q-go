package main

import (
	"encoding/csv"
	"log"
	"os"
	"strconv"
	"time"
)

type User struct {
	ID           int
	Login        string
	PasswordHash string
	Salt         string
	password     string

	LastLogin *LastLogin
}

type UserRepository struct {
	userById map[int]*User
}

var userRepository *UserRepository

func (r *UserRepository) Add(user *User) {
}

type LastLogin struct {
	Login     string
	IP        string
	CreatedAt time.Time
}

func (u *User) getLastLogin() *LastLogin {
	rows, err := db.Query(
		"SELECT login, ip, created_at FROM login_log WHERE succeeded = 1 AND user_id = ? ORDER BY id DESC LIMIT 2",
		u.ID,
	)

	if err != nil {
		return nil
	}

	defer rows.Close()
	for rows.Next() {
		u.LastLogin = &LastLogin{}
		err = rows.Scan(&u.LastLogin.Login, &u.LastLogin.IP, &u.LastLogin.CreatedAt)
		if err != nil {
			u.LastLogin = nil
			return nil
		}
	}

	return u.LastLogin
}

func initUsers() {
	userRepository = &UserRepository{userById: make(map[int]*User)}

	file, err := os.Open("dummy_users.tsv")
	if err != nil {
		log.Fatal(err)
	}
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 5
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	for _, rec := range records {
		id, err := strconv.Atoi(rec[0])
		if err != nil {
			log.Println(rec)
			log.Fatal(err)
		}
		name := rec[1]
		pass := rec[2]
		log.Printf("id: %v, name: %v, pass: %v", id, name, pass)
		userRepository.Add(&User{ID: id, Login: name, password: pass})
	}
}
