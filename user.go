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
	userById   map[int]*User
	userByName map[string]*User
}

var userRepository *UserRepository

func NewUserRepository() *UserRepository {
	return &UserRepository{
		userById:   make(map[int]*User),
		userByName: make(map[string]*User),
	}
}

func (r *UserRepository) Add(user *User) {
	r.userById[user.ID] = user
	r.userByName[user.Login] = user
}

func (r *UserRepository) ByName(name string) *User {
	return r.userByName[name]
}
func (r *UserRepository) ById(id int) *User {
	return r.userById[id]
}

type LastLogin struct {
	Login     string
	IP        string
	CreatedAt time.Time
}

func (u *User) getLastLogin() *LastLogin {
	hist := loginHistory.ByName(u.Login)
	u.LastLogin = &LastLogin{}
	if hist == nil || len(hist) < 2 {
		return nil
	}

	var l *UserLogin = nil
	current := false
	for i := len(hist) - 1; i >= 0; i-- {
		if !hist[i].Success {
			continue
		}
		if current {
			l = hist[i]
			break
		}
		current = true
	}
	if l == nil {
		return nil
	}
	u.LastLogin.Login = l.Login
	u.LastLogin.CreatedAt = l.CreatedAt
	u.LastLogin.IP = l.Ip
	return u.LastLogin
}

func initUsers() {
	userRepository = NewUserRepository()

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
		//log.Printf("id: %v, name: %v, pass: %v", id, name, pass)
		userRepository.Add(&User{ID: id, Login: name, password: pass})
	}
}
